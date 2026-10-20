package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/mp4"
)

// ScreenRecording consumes compressed packets, independently of display/decode.
// The bounded queue never stalls preview or silently drops dependent H.264 frames.
type ScreenRecording struct {
	mu          sync.Mutex
	packets     chan recordingPacket
	done        chan struct{}
	queuedBytes int
	finished    bool
	state       ScreenRecordingState
	stoppedAt   time.Time
	target      string
	audioConfig []byte
}

type ScreenRecordingState struct {
	StartedAt time.Time
	Duration  time.Duration
	Stopping  bool
	Switching bool
	Path      string // nonempty only when a playable recording was saved
	Err       error
}

type recordingPacket struct {
	data        []byte
	pts         uint64 // microseconds, with scrcpy flag bits removed
	config, key bool
	audio       bool
	at          time.Time
}

var errRecordingEnded = errors.New("录屏自动结束")

func (s *ScreenStream) StartRecording(path string) (*ScreenRecording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready || s.ctx.Err() != nil || s.err != nil {
		return nil, fmt.Errorf("请先连接设备投屏，再开始录屏")
	}
	if s.recording != nil {
		select {
		case <-s.recording.Done():
		default:
			return nil, fmt.Errorf("上一段录屏尚未保存完成")
		}
	}
	r := &ScreenRecording{packets: make(chan recordingPacket, 240), done: make(chan struct{}), target: path}
	if s.options.SystemAudio {
		if err := validateScreenAAC(s.audioConfig); err != nil {
			return nil, err
		}
		r.audioConfig = bytes.Clone(s.audioConfig)
	}
	if len(s.videoConfig) != 0 {
		r.offer(s.videoConfig, 0, true, false)
	}
	// Static screens may not produce another natural keyframe promptly, even
	// with a short encoder interval. Reset only the video encoder to start a
	// fresh random-access frame; the audio connection and clock stay active.
	if s.control != nil {
		select {
		case s.control <- []byte{17}: // scrcpy 3.3.4 RESET_VIDEO
		default:
			return nil, fmt.Errorf("投屏控制繁忙，无法准备录屏关键帧，请稍后重试")
		}
	}
	s.recording = r
	go r.run()
	return r, nil
}

func (r *ScreenRecording) Done() <-chan struct{} { return r.done }
func (r *ScreenRecording) Stop()                 { r.stop(nil) }
func (r *ScreenRecording) State() ScreenRecordingState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *ScreenRecording) stop(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked(err)
}

func (r *ScreenRecording) stopLocked(err error) {
	if r.finished {
		return
	}
	if r.state.Err == nil && err != nil {
		r.state.Err = err
	}
	if !r.state.Stopping {
		r.state.Stopping = true
		r.stoppedAt = time.Now()
		close(r.packets)
	}
}

func (r *ScreenRecording) offer(data []byte, pts uint64, config, key bool) {
	r.offerPacket(recordingPacket{data: data, pts: pts, config: config, key: key, at: time.Now()})
}

func (r *ScreenRecording) offerPacket(packet recordingPacket) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Stopping {
		return
	}
	if len(r.packets) == cap(r.packets) || r.queuedBytes+len(packet.data) > 32<<20 {
		r.stopLocked(fmt.Errorf("磁盘写入跟不上录屏，已停止录制；请更换保存位置后重试"))
		return
	}
	r.queuedBytes += len(packet.data)
	packet.data = bytes.Clone(packet.data)
	r.packets <- packet
}

func (s *ScreenStream) recordPacket(data []byte, flags uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config := flags&(uint64(1)<<63) != 0
	if config {
		s.videoConfig = append(s.videoConfig[:0], data...)
	}
	if s.recording != nil {
		s.recording.offer(data, flags&((uint64(1)<<62)-1), config, flags&(uint64(1)<<62) != 0)
	}
}

func (s *ScreenStream) stopRecording(err error) *ScreenRecording {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recording != nil {
		s.recording.stop(err)
	}
	return s.recording
}

func (r *ScreenRecording) run() {
	defer close(r.done)
	err := r.save()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked(nil)
	r.state.Err = errors.Join(r.state.Err, err)
	r.state.Switching = false
	r.finished = true
	// Release any remaining packets after an early mux/write failure.
	for range r.packets {
	}
	r.queuedBytes = 0
}

func (r *ScreenRecording) save() error {
	file, err := os.CreateTemp(filepath.Dir(r.target), ".loghelp-recording-*.mp4")
	if err != nil {
		return fmt.Errorf("创建录屏文件：%w", err)
	}
	temporary := file.Name()
	closed := false
	defer func() {
		if !closed {
			file.Close()
		}
	}()
	mux := recordingMux{file: file, index: &mp4.TfraBox{Version: 1, TrackID: 1}, audioConfig: r.audioConfig, audioIndex: &mp4.TfraBox{Version: 1, TrackID: 2}}
	err = r.consume(&mux)
	if err == nil && mux.pending != nil && len(r.audioConfig) != 0 && mux.audioSamples == 0 {
		r.stop(fmt.Errorf("未收到可保存的系统声音数据"))
	}
	if err == nil {
		err = mux.finish(r.stopTime())
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	closed = true
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary, r.target)
	}
	if err != nil {
		if mux.samples == 0 {
			if removeErr := os.Remove(temporary); removeErr != nil {
				return fmt.Errorf("录屏未保存：%w；清理临时文件 %s：%v", err, temporary, removeErr)
			}
			return fmt.Errorf("录屏未保存：%w", err)
		}
		return fmt.Errorf("保存录屏失败：%w；临时文件保留在 %s", err, temporary)
	}
	r.mu.Lock()
	r.state.Path = r.target
	r.state.Duration = time.Duration(mux.duration) * time.Microsecond
	r.mu.Unlock()
	return nil
}

func (r *ScreenRecording) stopTime() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stoppedAt.IsZero() {
		return time.Now()
	}
	return r.stoppedAt
}

func (r *ScreenRecording) consume(mux *recordingMux) error {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			if mux.pending == nil {
				return fmt.Errorf("等待视频关键帧超时，请重连投屏后重试")
			}
			if mux.waitingKey {
				r.stop(fmt.Errorf("切换屏幕后等待视频关键帧超时，已结束录制"))
				return nil
			}
		case packet, ok := <-r.packets:
			if !ok {
				return nil
			}
			r.mu.Lock()
			r.queuedBytes -= len(packet.data)
			r.mu.Unlock()
			wasWaiting := mux.waitingKey
			if err := mux.add(packet); err != nil {
				// Invalid codec/timeline data still leaves the previous MP4 valid.
				if errors.Is(err, errRecordingEnded) {
					r.stop(err)
					return nil
				}
				return err
			}
			if mux.waitingKey && !wasWaiting {
				timer.Reset(15 * time.Second)
			} else if !mux.waitingKey && mux.pending != nil {
				timer.Stop()
			}
			r.mu.Lock()
			r.state.Switching = mux.waitingKey && mux.pending != nil
			if mux.pending != nil {
				if r.state.StartedAt.IsZero() {
					r.state.StartedAt = mux.pending.at
				}
				r.state.Duration = time.Duration(mux.pending.pts) * time.Microsecond
			}
			r.mu.Unlock()
		}
	}
}

// Each frame is one fragment: frame data is bounded and already-written fragments
// survive a stream interruption. avc3 carries parameter sets in each keyframe,
// so resolution changes and seeking do not require rewriting the MP4 header.
// scrcpy's low-latency encoder has no B frames. Only a codec configuration
// boundary permits rebasing timestamps after an encoder restart.
type recordingMux struct {
	file                *os.File
	init                *mp4.InitSegment
	config              []byte
	sps, pps            [][]byte
	pending             *recordingPacket
	sourcePTS           uint64
	lastDelta           uint64
	waitingKey          bool
	allowTimestampReset bool
	duration            uint64
	samples             uint32
	index               *mp4.TfraBox
	audioConfig         []byte
	audioIndex          *mp4.TfraBox
	audioSamples        uint64
	audioEnd            uint64
	originPTS           uint64
	sequence            uint32
}

func (m *recordingMux) add(packet recordingPacket) error {
	if packet.audio {
		return m.addAudio(packet)
	}
	if packet.config {
		m.allowTimestampReset = true
		if bytes.Equal(m.config, packet.data) {
			return nil
		}
		sps := avc.ExtractNalusOfTypeFromByteStream(avc.NALU_SPS, packet.data, false)
		pps := avc.ExtractNalusOfTypeFromByteStream(avc.NALU_PPS, packet.data, false)
		if len(sps) == 0 || len(pps) == 0 {
			return fmt.Errorf("%w：缺少 H.264 视频配置，请重连投屏后重试", errRecordingEnded)
		}
		if _, err := avc.CreateAVCDecConfRec(sps, pps, true); err != nil {
			return fmt.Errorf("%w：解析录屏视频配置：%v", errRecordingEnded, err)
		}
		m.config = packet.data
		m.sps, m.pps = sps, pps
		m.waitingKey = true
		return nil
	}
	if m.waitingKey && !packet.key {
		return nil
	}
	sourcePTS := packet.pts
	if m.pending == nil {
		if !packet.key {
			return nil
		}
		if len(m.sps) == 0 || len(m.pps) == 0 {
			return fmt.Errorf("缺少 H.264 视频配置，请重连投屏后重试")
		}
		m.init = mp4.CreateEmptyInit()
		track := m.init.AddEmptyTrack(1000000, "video", "und")
		if err := track.SetAVCDescriptor("avc3", m.sps, m.pps, true); err != nil {
			return fmt.Errorf("解析录屏视频配置：%w", err)
		}
		// Reserve 64-bit durations before writing; final rewrite has identical size.
		m.init.Moov.Mvhd.Version, track.Tkhd.Version, track.Mdia.Mdhd.Version = 1, 1, 1
		if len(m.audioConfig) != 0 {
			audioTrack := m.init.AddEmptyTrack(48000, "audio", "und")
			audioTrack.Mdia.Minf.Stbl.Stsd.AddChild(mp4.CreateAudioSampleEntryBox("mp4a", 2, 16, 48000, mp4.CreateEsdsBox(m.audioConfig)))
			audioTrack.Tkhd.Version, audioTrack.Mdia.Mdhd.Version = 1, 1
		}
		if err := m.init.Encode(m.file); err != nil {
			return fmt.Errorf("写入 MP4 文件头：%w", err)
		}
		packet.pts = 0
		m.originPTS = sourcePTS
	} else {
		var delta uint64
		if sourcePTS <= m.sourcePTS {
			if !m.allowTimestampReset {
				return fmt.Errorf("%w：设备视频时间戳异常", errRecordingEnded)
			}
			if !packet.key {
				m.waitingKey = true
				return nil
			}
			// Keep one monotonic MP4 timeline even if a restarted encoder starts
			// counting from zero. Preserve the real switching gap in that case.
			delta = uint64(max(int64(1), packet.at.Sub(m.pending.at).Microseconds(), int64(m.lastDelta)))
		} else {
			delta = sourcePTS - m.sourcePTS
		}
		if delta > uint64(^uint32(0)) {
			return fmt.Errorf("%w：设备视频时间间隔过长", errRecordingEnded)
		}
		if err := m.writePending(uint32(delta)); err != nil {
			return err
		}
		packet.pts = m.pending.pts + delta
		m.lastDelta = delta
	}
	if packet.key {
		// Include the current SPS/PPS at every random-access point, including
		// after switching back to a previously used screen configuration.
		payload := make([]byte, 0, len(m.config)+len(packet.data))
		payload = append(payload, m.config...)
		packet.data = append(payload, packet.data...)
	}
	// Conversion may mutate packet.data, which is private to this recorder.
	packet.data = avc.ConvertByteStreamToNaluSample(packet.data)
	m.pending = &packet
	m.sourcePTS = sourcePTS
	m.waitingKey, m.allowTimestampReset = false, false
	return nil
}

func (m *recordingMux) writePending(duration uint32) error {
	p := m.pending
	fragment, err := mp4.CreateFragment(m.sequence+1, 1)
	if err != nil {
		return err
	}
	flags := mp4.NonSyncSampleFlags
	if p.key {
		flags = mp4.SyncSampleFlags
	}
	decodeTime := p.pts
	fragment.AddFullSample(mp4.FullSample{Sample: mp4.Sample{Flags: flags, Dur: duration, Size: uint32(len(p.data))}, DecodeTime: decodeTime, Data: p.data})
	offset, err := m.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if err := fragment.Encode(m.file); err != nil {
		return fmt.Errorf("写入录屏视频帧：%w", err)
	}
	if p.key {
		m.index.Entries = append(m.index.Entries, mp4.TfraEntry{Time: decodeTime, MoofOffset: uint64(offset), TrafNumber: 1, TrunNumber: 1, SampleNumber: 1})
	}
	m.samples++
	m.sequence++
	m.duration = decodeTime + uint64(duration)
	return nil
}

func (m *recordingMux) addAudio(packet recordingPacket) error {
	if packet.config {
		if !bytes.Equal(m.audioConfig, packet.data) {
			return fmt.Errorf("%w：设备音频配置发生变化", errRecordingEnded)
		}
		return nil
	}
	if m.pending == nil || len(m.audioConfig) == 0 || packet.pts < m.originPTS {
		return nil
	}
	// Both streams use the Android monotonic clock. Keep the original audio
	// offset from the first recorded video frame, including a late audio start.
	decodeTime := (packet.pts - m.originPTS) * 48000 / 1000000
	decodeTime = max(decodeTime, m.audioEnd)
	fragment, err := mp4.CreateFragment(m.sequence+1, 2)
	if err != nil {
		return err
	}
	fragment.AddFullSample(mp4.FullSample{Sample: mp4.Sample{Flags: mp4.SyncSampleFlags, Dur: 1024, Size: uint32(len(packet.data))}, DecodeTime: decodeTime, Data: packet.data})
	offset, err := m.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if err := fragment.Encode(m.file); err != nil {
		return fmt.Errorf("写入系统声音：%w", err)
	}
	if len(m.audioIndex.Entries) == 0 || decodeTime-m.audioIndex.Entries[len(m.audioIndex.Entries)-1].Time >= 48000 {
		m.audioIndex.Entries = append(m.audioIndex.Entries, mp4.TfraEntry{Time: decodeTime, MoofOffset: uint64(offset), TrafNumber: 1, TrunNumber: 1, SampleNumber: 1})
	}
	m.audioEnd = decodeTime + 1024
	m.audioSamples++
	m.sequence++
	return nil
}

func (m *recordingMux) finish(stoppedAt time.Time) error {
	if m.pending == nil {
		return fmt.Errorf("尚未收到视频关键帧，没有可保存的录屏")
	}
	duration := max(int64(1), stoppedAt.Sub(m.pending.at).Microseconds())
	if err := m.writePending(uint32(min(duration, int64(^uint32(0))))); err != nil {
		return err
	}
	mfra := &mp4.MfraBox{}
	if err := mfra.AddChild(m.index); err != nil {
		return err
	}
	if m.audioSamples != 0 {
		if err := mfra.AddChild(m.audioIndex); err != nil {
			return err
		}
	}
	footer := &mp4.MfroBox{}
	if err := mfra.AddChild(footer); err != nil {
		return err
	}
	footer.ParentSize = uint32(mfra.Size())
	if err := mfra.Encode(m.file); err != nil {
		return err
	}
	movieDuration := m.duration * uint64(m.init.Moov.Mvhd.Timescale) / 1000000
	m.init.Moov.Mvhd.Duration = movieDuration
	m.init.Moov.Trak.Tkhd.Duration = movieDuration
	m.init.Moov.Trak.Mdia.Mdhd.Duration = m.duration
	if len(m.init.Moov.Traks) > 1 {
		audioTrack := m.init.Moov.Traks[1]
		audioTrack.Mdia.Mdhd.Duration = m.audioEnd
		audioTrack.Tkhd.Duration = m.audioEnd * uint64(m.init.Moov.Mvhd.Timescale) / 48000
		m.init.Moov.Mvhd.Duration = max(movieDuration, audioTrack.Tkhd.Duration)
	}
	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return m.init.Encode(m.file)
}
