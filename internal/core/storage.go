package core

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"loghelp/internal/support"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RecordOptions struct {
	Directory                     string
	SegmentBytes, QuotaBytes      int64
	SegmentMinutes, RetentionDays int
	Cleanup                       bool
	DeviceInfo                    map[string]string
	Recovery                      *RecordingRecovery
}
type RecordingRecovery struct {
	PreviousDirectory string    `json:"previous_directory"`
	LastSeq           uint64    `json:"last_seq"`
	Unwritten         uint64    `json:"unwritten"`
	LastReceived      time.Time `json:"last_received,omitempty"`
	FailedAt          time.Time `json:"failed_at"`
	ResumedAt         time.Time `json:"resumed_at"`
	Reason            string    `json:"reason"`
}
type Manifest struct {
	Format       string             `json:"format"`
	Version      int                `json:"version"`
	Serial       string             `json:"serial"`
	Buffers      []string           `json:"buffers"`
	Started      time.Time          `json:"started"`
	Ended        *time.Time         `json:"ended,omitempty"`
	Timezone     string             `json:"timezone"`
	Segments     []string           `json:"segments"`
	Protected    bool               `json:"protected"`
	Active       bool               `json:"active"`
	LastSeq      uint64             `json:"last_seq"`
	FirstSeq     uint64             `json:"first_seq,omitempty"`
	Error        string             `json:"error,omitempty"`
	DeviceInfo   map[string]string  `json:"device_info,omitempty"`
	Recovery     *RecordingRecovery `json:"recovery,omitempty"`
	Unwritten    uint64             `json:"unwritten,omitempty"`
	LastReceived time.Time          `json:"last_received,omitempty"`
	FailedAt     time.Time          `json:"failed_at,omitempty"`
}
type RecordStatus struct {
	Active, Paused         bool
	Directory, Error       string
	Bytes                  int64
	LastSeq, Unwritten     uint64
	LastReceived, FailedAt time.Time
}
type Recorder struct {
	mu                       sync.Mutex
	queue                    chan *Entry
	done                     chan struct{}
	status                   RecordStatus
	queuedBytes              int64
	closed                   bool
	manifest                 Manifest
	options                  RecordOptions
	file                     *os.File
	writer                   *bufio.Writer
	segmentBytes, totalBytes int64
	segmentStarted           time.Time
	durableSeq               uint64
	durableReceived          time.Time
	budget                   *directoryBudget
}

var recordingDirectories = struct {
	sync.Mutex
	active map[string]bool
}{active: map[string]bool{}}

func ProtectSession(directory string, protect bool) error {
	path := filepath.Join(directory, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var manifest Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.Format != "loghelp" || manifest.Version != 1 {
		return fmt.Errorf("不支持的会话格式")
	}
	if manifest.Active {
		return fmt.Errorf("不能更改活动会话的保留标记")
	}
	manifest.Protected = protect
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(path, data)
}

func recordOptions(c Config) RecordOptions {
	return RecordOptions{Directory: c.Directory, SegmentBytes: c.SegmentBytes, QuotaBytes: c.QuotaBytes, SegmentMinutes: c.SegmentMinutes, RetentionDays: c.RetentionDays, Cleanup: c.Cleanup}
}
func OptionsFromConfig(c Config) RecordOptions { return recordOptions(c) }
func directorySize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
func NewRecorder(serial string, buffers []string, options RecordOptions) (*Recorder, error) {
	if options.Directory == "" {
		return nil, fmt.Errorf("请先选择录制目录")
	}
	root, err := filepath.Abs(options.Directory)
	if err != nil {
		return nil, err
	}
	options.Directory = root
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if options.SegmentBytes < 1024 {
		options.SegmentBytes = 100 << 20
	}
	if options.QuotaBytes <= 0 {
		options.QuotaBytes = 2 << 30
	}
	if options.Cleanup {
		if err = CleanupRecordings(root, options.RetentionDays, options.QuotaBytes); err != nil {
			return nil, err
		}
	}
	size, err := directorySize(root)
	if err != nil {
		return nil, err
	}
	if size >= options.QuotaBytes {
		return nil, fmt.Errorf("目录已达到容量上限（%d MiB）", options.QuotaBytes>>20)
	}
	var random [6]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, serial)
	dir := filepath.Join(root, safe, time.Now().Format("2006-01-02"), time.Now().Format("150405")+"-"+hex.EncodeToString(random[:]))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	r := &Recorder{queue: make(chan *Entry, 4096), done: make(chan struct{}), status: RecordStatus{Active: true, Directory: dir}, options: options, totalBytes: size, manifest: Manifest{Format: "loghelp", Version: 1, Serial: serial, Buffers: buffers, Started: time.Now(), Timezone: time.Now().Location().String(), Active: true, DeviceInfo: options.DeviceInfo}}
	if options.Recovery != nil {
		recovery := *options.Recovery
		recovery.ResumedAt = r.manifest.Started
		r.manifest.Recovery = &recovery
	}
	r.budget, err = acquireBudget(root)
	if err != nil {
		return nil, err
	}
	if err = r.rotate(); err != nil {
		r.releaseBudget()
		return nil, err
	}
	recordingDirectories.Lock()
	recordingDirectories.active[dir] = true
	recordingDirectories.Unlock()
	if recovery := r.manifest.Recovery; recovery != nil {
		message := fmt.Sprintf("录制故障恢复：上一会话 %s，最后持久化序号 %d（接收时间 %s），已确认未写入 %d 条；故障 %s，恢复 %s。间隔内日志未回填，可能存在缺口。原因：%s", recovery.PreviousDirectory, recovery.LastSeq, recovery.LastReceived.Format(time.RFC3339Nano), recovery.Unwritten, recovery.FailedAt.Format(time.RFC3339Nano), recovery.ResumedAt.Format(time.RFC3339Nano), recovery.Reason)
		r.Append(&Entry{Time: recovery.ResumedAt, Received: recovery.ResumedAt, Serial: serial, Event: true, Tag: "LogHelp", Level: "W", Raw: message, Message: message})
	}
	go r.run()
	return r, nil
}
func (r *Recorder) saveManifest() error {
	data, err := json.MarshalIndent(r.manifest, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(r.status.Directory, "manifest.json"), data)
}
func (r *Recorder) flush() error {
	if r.writer == nil {
		return nil
	}
	if err := r.writer.Flush(); err != nil {
		return err
	}
	if err := r.file.Sync(); err != nil {
		return err
	}
	r.mu.Lock()
	r.status.LastSeq = r.durableSeq
	r.status.LastReceived = r.durableReceived
	r.mu.Unlock()
	return nil
}
func (r *Recorder) rotate() error {
	if r.file != nil {
		if err := r.flush(); err != nil {
			return err
		}
		if err := r.file.Close(); err != nil {
			return err
		}
	}
	name := fmt.Sprintf("%06d.jsonl", len(r.manifest.Segments)+1)
	f, err := os.OpenFile(filepath.Join(r.status.Directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	r.file = f
	r.writer = bufio.NewWriterSize(f, 128<<10)
	r.segmentBytes = 0
	r.segmentStarted = time.Now()
	r.manifest.Segments = append(r.manifest.Segments, name)
	if err = r.saveManifest(); err != nil {
		f.Close()
		return err
	}
	return nil
}
func (r *Recorder) Append(e *Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.status.Paused {
		return
	}
	if r.queuedBytes+e.Size() > 16<<20 {
		r.failLocked("磁盘写入跟不上：队列超过 16 MiB", 1)
		return
	}
	select {
	case r.queue <- e:
		r.queuedBytes += e.Size()
	default:
		r.failLocked("磁盘写入跟不上：队列已满", 1)
	}
}
func (r *Recorder) failLocked(message string, lost uint64) {
	if r.status.Error == "" {
		support.Record("ERROR", "recording.failed", message)
		r.status.Error = message
		r.status.FailedAt = time.Now()
	}
	r.status.Unwritten += lost
	r.status.Active = false
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
}
func (r *Recorder) Pause(pause bool) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.status.Paused = pause
	r.mu.Unlock()
	message := "录制继续，前段有暂停间隔"
	if pause {
		message = "录制暂停：此后条目暂不写盘"
	}
	e := &Entry{Time: time.Now(), Received: time.Now(), Serial: r.manifest.Serial, Event: true, Tag: "LogHelp", Level: "I", Message: message, Raw: message}
	r.mu.Lock()
	if !r.closed {
		select {
		case r.queue <- e:
			r.queuedBytes += e.Size()
		default:
			r.failLocked("录制事件未能入队", 1)
		}
	}
	r.mu.Unlock()
}
func (r *Recorder) Status() RecordStatus { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (r *Recorder) Stop() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	<-r.done
	status := r.Status()
	if status.Error != "" {
		return fmt.Errorf("%s", status.Error)
	}
	return nil
}
func (r *Recorder) run() {
	defer close(r.done)
	defer r.releaseBudget()
	defer func() {
		recordingDirectories.Lock()
		delete(recordingDirectories.active, r.status.Directory)
		recordingDirectories.Unlock()
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	failed := false
	pendingCount := uint64(0)
	setError := func(err error) {
		r.mu.Lock()
		r.failLocked(err.Error(), pendingCount)
		r.mu.Unlock()
		pendingCount = 0
		failed = true
	}
	for {
		select {
		case e, ok := <-r.queue:
			if !ok {
				goto finish
			}
			r.mu.Lock()
			r.queuedBytes -= e.Size()
			r.mu.Unlock()
			if failed {
				r.mu.Lock()
				r.status.Unwritten++
				r.mu.Unlock()
				continue
			}
			data, err := json.Marshal(e)
			if err != nil {
				setError(err)
				continue
			}
			data = append(data, '\n')
			if err = r.reserve(int64(len(data))); err != nil {
				if flushErr := r.flush(); flushErr == nil {
					pendingCount = 0
				} else {
					err = flushErr
				}
				pendingCount++
				setError(err)
				continue
			}
			if r.segmentBytes > 0 && (r.segmentBytes >= r.options.SegmentBytes || r.options.SegmentMinutes > 0 && time.Since(r.segmentStarted) >= time.Duration(r.options.SegmentMinutes)*time.Minute) {
				if err = r.rotate(); err != nil {
					pendingCount++
					setError(err)
					continue
				}
				pendingCount = 0
			}
			pendingCount++
			if _, err = r.writer.Write(data); err != nil {
				setError(err)
				continue
			}
			if e.Seq > 0 {
				if r.manifest.FirstSeq == 0 {
					r.manifest.FirstSeq = e.Seq
				}
				r.durableSeq = e.Seq
				r.durableReceived = e.Received
			}
			r.segmentBytes += int64(len(data))
			r.totalBytes += int64(len(data))
			r.mu.Lock()
			r.status.Bytes += int64(len(data))
			r.mu.Unlock()
		case <-ticker.C:
			if !failed {
				if err := r.flush(); err != nil {
					setError(err)
				} else {
					pendingCount = 0
				}
			}
		}
	}
finish:
	if !failed {
		if err := r.flush(); err != nil {
			setError(err)
		}
	}
	if r.file != nil {
		if err := r.file.Close(); err != nil && !failed {
			setError(err)
		}
	}
	now := time.Now()
	r.manifest.Ended = &now
	r.manifest.Active = false
	status := r.Status()
	r.manifest.LastSeq = status.LastSeq
	r.manifest.Error = status.Error
	r.manifest.Unwritten = status.Unwritten
	r.manifest.LastReceived = status.LastReceived
	r.manifest.FailedAt = status.FailedAt
	if err := r.saveManifest(); err != nil {
		r.mu.Lock()
		if r.status.Error == "" {
			r.status.Error = "保存会话元数据失败：" + err.Error()
			r.status.FailedAt = time.Now()
		}
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.status.Active = false
	r.mu.Unlock()
}

// Cleanup only removes recognized, closed sessions wholly inside root. Symlinks
// and unknown files are never cleanup candidates.
func CleanupRecordings(root string, days int, quota int64) error {
	budgets.Lock()
	budget := budgets.items[root]
	budgets.Unlock()
	if budget != nil {
		budget.Lock()
		defer budget.Unlock()
	}
	removed, err := cleanupManaged(root, days, quota)
	if budget != nil {
		budget.total -= removed
	}
	return err
}

func cleanupManaged(root string, days int, quota int64) (int64, error) {
	var removed int64
	abs, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	type session struct {
		path    string
		started time.Time
	}
	var sessions []session
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() || d.Name() != "manifest.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m Manifest
		if json.Unmarshal(data, &m) != nil || m.Format != "loghelp" || m.Version != 1 || m.Active || m.Protected || m.Ended == nil {
			return nil
		}
		dir := filepath.Dir(path)
		recordingDirectories.Lock()
		active := recordingDirectories.active[dir]
		recordingDirectories.Unlock()
		if active {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		names := map[string]bool{"manifest.json": true}
		for _, s := range m.Segments {
			names[s] = true
		}
		for _, e := range entries {
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !names[e.Name()] {
				return nil
			}
		}
		sessions = append(sessions, session{dir, m.Started})
		return nil
	})
	if err != nil {
		return 0, err
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].started.Before(sessions[j].started) })
	total, err := directorySize(abs)
	if err != nil {
		return 0, err
	}
	for _, s := range sessions {
		expired := days > 0 && s.started.Before(time.Now().AddDate(0, 0, -days))
		if !expired && (quota < 0 || total <= quota) {
			continue
		}
		rel, err := filepath.Rel(abs, s.path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return removed, fmt.Errorf("清理路径越界")
		}
		size, err := directorySize(s.path)
		if err != nil {
			return removed, err
		}
		if err = os.RemoveAll(s.path); err != nil {
			return removed, err
		}
		total -= size
		removed += size
	}
	return removed, nil
}
