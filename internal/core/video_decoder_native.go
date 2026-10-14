//go:build windows || darwin || linux

package core

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// These prefixes are the public FFmpeg 7.1.1 ABI (libavutil 59 / avcodec 61).
// The bundled DLLs are hash-checked before loading and version-checked below.
type avFramePrefix struct {
	Data                                                          [8]uintptr
	LineSize                                                      [8]int32
	ExtendedData                                                  uintptr
	Width, Height, Samples, Format, Key, PictureType              int32
	Aspect                                                        [2]int32
	PTS, DTS                                                      int64
	TimeBase                                                      [2]int32
	Quality                                                       int32
	Opaque                                                        uintptr
	Repeat, Interlaced, TopField, PaletteChanged, SampleRate      int32
	Buffers                                                       [8]uintptr
	ExtendedBuffers                                               uintptr
	ExtendedCount                                                 int32
	SideData                                                      uintptr
	SideCount, Flags, ColorRange, Primaries, Transfer, ColorSpace int32
}
type avPacketPrefix struct {
	Buffer                   uintptr
	PTS, DTS                 int64
	Data                     uintptr
	Size, StreamIndex, Flags int32
}
type ffmpegAPI struct {
	findDecoder               func(string) uintptr
	allocContext              func(uintptr) uintptr
	openContext               func(uintptr, uintptr, uintptr) int32
	freeContext               func(*uintptr)
	packetAlloc               func() uintptr
	packetFree                func(*uintptr)
	packetNew                 func(uintptr, int32) int32
	packetUnref               func(uintptr)
	sendPacket                func(uintptr, uintptr) int32
	receiveFrame              func(uintptr, uintptr) int32
	frameAlloc                func() uintptr
	frameFree                 func(*uintptr)
	frameUnref                func(uintptr)
	optionInt                 func(uintptr, string, int64, int32) int32
	optionString              func(uintptr, string, string, int32) int32
	versionInfo               func() string
	codecVersion, utilVersion func() uint32
	logLevel                  func(int32)
}

var nativeFFmpeg struct {
	once sync.Once
	api  ffmpegAPI
	err  error
}

func loadFFmpeg(directory string) (*ffmpegAPI, error) {
	nativeFFmpeg.once.Do(func() {
		symbol, err := loadVideoLibraries(directory)
		if err != nil {
			nativeFFmpeg.err = err
			return
		}
		api := &nativeFFmpeg.api
		bindings := []struct {
			library, name string
			target        any
		}{
			{"avcodec-61.dll", "avcodec_find_decoder_by_name", &api.findDecoder},
			{"avcodec-61.dll", "avcodec_alloc_context3", &api.allocContext},
			{"avcodec-61.dll", "avcodec_open2", &api.openContext},
			{"avcodec-61.dll", "avcodec_free_context", &api.freeContext},
			{"avcodec-61.dll", "av_packet_alloc", &api.packetAlloc},
			{"avcodec-61.dll", "av_packet_free", &api.packetFree},
			{"avcodec-61.dll", "av_new_packet", &api.packetNew},
			{"avcodec-61.dll", "av_packet_unref", &api.packetUnref},
			{"avcodec-61.dll", "avcodec_send_packet", &api.sendPacket},
			{"avcodec-61.dll", "avcodec_receive_frame", &api.receiveFrame},
			{"avcodec-61.dll", "avcodec_version", &api.codecVersion},
			{"avutil-59.dll", "av_frame_alloc", &api.frameAlloc},
			{"avutil-59.dll", "av_frame_free", &api.frameFree},
			{"avutil-59.dll", "av_frame_unref", &api.frameUnref},
			{"avutil-59.dll", "av_opt_set_int", &api.optionInt},
			{"avutil-59.dll", "av_opt_set", &api.optionString},
			{"avutil-59.dll", "av_version_info", &api.versionInfo},
			{"avutil-59.dll", "avutil_version", &api.utilVersion},
			{"avutil-59.dll", "av_log_set_level", &api.logLevel},
		}
		for _, binding := range bindings {
			address, err := symbol(binding.library, binding.name)
			if err != nil {
				nativeFFmpeg.err = fmt.Errorf("投屏解码库缺少 %s：%w", binding.name, err)
				return
			}
			purego.RegisterFunc(binding.target, address)
		}
		if api.codecVersion()>>16 != 61 || api.utilVersion()>>16 != 59 {
			nativeFFmpeg.err = fmt.Errorf("直接投屏解码库版本不匹配，需要 FFmpeg 7 的 avcodec 61 / avutil 59")
			return
		}
		api.logLevel(16)
	})
	return &nativeFFmpeg.api, nativeFFmpeg.err
}

type videoDecoder struct {
	api                    *ffmpegAPI
	context, packet, frame uintptr
}

func newVideoDecoder(directory string) (videoDecode, error) {
	if os.Getenv("LOGHELP_FFMPEG") != "" {
		return newProcessVideoDecoder()
	}
	api, err := loadFFmpeg(directory)
	if err != nil {
		fallback, fallbackErr := newProcessVideoDecoder()
		if fallbackErr == nil {
			return fallback, nil
		}
		return nil, fmt.Errorf("%v；%w", err, fallbackErr)
	}
	codec := api.findDecoder("h264")
	if codec == 0 {
		return nil, fmt.Errorf("投屏解码库不支持 H.264")
	}
	d := &videoDecoder{api: api, context: api.allocContext(codec), packet: api.packetAlloc(), frame: api.frameAlloc()}
	if d.context == 0 || d.packet == 0 || d.frame == 0 {
		d.close()
		return nil, fmt.Errorf("分配投屏解码器失败")
	}
	// One decoding thread avoids frame-thread buffering and its extra latency.
	if api.optionInt(d.context, "threads", 1, 0) < 0 || api.optionString(d.context, "flags", "+low_delay", 0) < 0 {
		d.close()
		return nil, fmt.Errorf("无法设置低延迟解码")
	}
	if result := api.openContext(d.context, codec, 0); result < 0 {
		d.close()
		return nil, fmt.Errorf("打开 H.264 解码器失败：%d", result)
	}
	return d, nil
}

func (d *videoDecoder) close() {
	if d.frame != 0 {
		d.api.frameFree(&d.frame)
	}
	if d.packet != 0 {
		d.api.packetFree(&d.packet)
	}
	if d.context != 0 {
		d.api.freeContext(&d.context)
	}
}

func (d *videoDecoder) decode(data []byte, stream *ScreenStream) error {
	api := d.api
	api.packetUnref(d.packet)
	if result := api.packetNew(d.packet, int32(len(data))); result < 0 {
		return fmt.Errorf("分配视频数据失败：%d", result)
	}
	packet := (*avPacketPrefix)(unsafe.Pointer(d.packet))
	copy(unsafe.Slice((*byte)(unsafe.Pointer(packet.Data)), len(data)), data)
	if result := api.sendPacket(d.context, d.packet); result < 0 {
		return fmt.Errorf("H.264 数据解码失败：%d", result)
	}
	for {
		result := api.receiveFrame(d.context, d.frame)
		if result == -11 || result == -541478725 {
			return nil
		}
		if result < 0 {
			return fmt.Errorf("读取视频帧失败：%d", result)
		}
		frame := (*avFramePrefix)(unsafe.Pointer(d.frame))
		if err := d.publish(frame, stream); err != nil {
			api.frameUnref(d.frame)
			return err
		}
		api.frameUnref(d.frame)
	}
}

func (d *videoDecoder) publish(frame *avFramePrefix, stream *ScreenStream) error {
	w, h := int(frame.Width), int(frame.Height)
	if w <= 0 || h <= 0 || w > 2048 || h > 2048 || w*h > 2_000_000 {
		return fmt.Errorf("投屏尺寸不支持：%d × %d", w, h)
	}
	if frame.Format != 0 && frame.Format != 12 {
		return fmt.Errorf("投屏像素格式不支持：%d", frame.Format)
	}
	var planes [3][]byte
	for i := 0; i < 3; i++ {
		height, width := h, w
		if i > 0 {
			height, width = (h+1)/2, (w+1)/2
		}
		stride := int(frame.LineSize[i])
		if frame.Data[i] == 0 || stride < width || stride > 16384 {
			return fmt.Errorf("视频帧平面 %d 无效", i)
		}
		planes[i] = unsafe.Slice((*byte)(unsafe.Pointer(frame.Data[i])), stride*(height-1)+width)
	}
	pixels := stream.acquirePixels(w * h * 4)
	// Pack Y/U/V into one texture. Color conversion runs on the GPU shader,
	// avoiding a full CPU RGB conversion and three texture uploads each frame.
	for y := 0; y < h; y++ {
		yRow := planes[0][y*int(frame.LineSize[0]):]
		uRow := planes[1][y/2*int(frame.LineSize[1]):]
		vRow := planes[2][y/2*int(frame.LineSize[2]):]
		row := pixels[y*w*4 : (y+1)*w*4]
		for x := 0; x < w; x++ {
			offset := x * 4
			row[offset], row[offset+1], row[offset+2], row[offset+3] = yRow[x], uRow[x/2], vRow[x/2], 255
		}
	}
	stream.publish(&VideoFrame{Pixels: pixels, Size: image.Pt(w, h), FullRange: frame.ColorRange == 2 || frame.Format == 12, BT709: frame.ColorSpace == 1})
	runtime.KeepAlive(d)
	return nil
}
