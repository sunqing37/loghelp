package core

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Portable decoding without C headers, CGO or library ABI assumptions. PAM
// headers give the dimensions of every frame, including after rotation.
type processVideoDecoder struct {
	input   io.WriteCloser
	output  io.ReadCloser
	command *exec.Cmd
	cancel  context.CancelFunc
	log     *streamLog
	done    chan struct{}
	mu      sync.Mutex
	err     error
	started bool
}

func newProcessVideoDecoder() (*processVideoDecoder, error) {
	path := strings.TrimSpace(os.Getenv("LOGHELP_FFMPEG"))
	if path == "" {
		if executable, err := os.Executable(); err == nil {
			name := "ffmpeg"
			if filepath.Ext(executable) == ".exe" {
				name += ".exe"
			}
			candidate := filepath.Join(filepath.Dir(executable), "runtime", name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				path = candidate
			}
		}
	}
	if path == "" {
		if runtime.GOOS == "darwin" {
			for _, candidate := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"} {
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					path = candidate
					break
				}
			}
		}
	}
	if path == "" {
		var err error
		path, err = exec.LookPath("ffmpeg")
		if err != nil {
			return nil, fmt.Errorf("未找到视频解码器 FFmpeg；请安装 ffmpeg、放入程序旁 runtime 目录，或通过 LOGHELP_FFMPEG 指定路径")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	logLevel := "error"
	if os.Getenv("LOGHELP_MIRROR_DEBUG") == "1" {
		logLevel = "debug"
	}
	command := exec.CommandContext(ctx, path, "-hide_banner", "-loglevel", logLevel, "-probesize", "32", "-analyzeduration", "1", "-flags", "low_delay", "-threads", "1", "-f", "h264", "-i", "pipe:0", "-an", "-sn", "-dn", "-threads", "1", "-fps_mode", "passthrough", "-c:v", "pam", "-pix_fmt", "rgba", "-f", "image2pipe", "pipe:1")
	configureProcess(command)
	log := &streamLog{buffer: limitedBuffer{limit: 16 << 10}}
	command.Stderr = log
	input, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建解码输入：%w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		cancel()
		return nil, fmt.Errorf("创建解码输出：%w", err)
	}
	if err := command.Start(); err != nil {
		input.Close()
		output.Close()
		cancel()
		return nil, fmt.Errorf("启动 FFmpeg 解码器：%w", err)
	}
	return &processVideoDecoder{input: input, output: output, command: command, cancel: cancel, log: log, done: make(chan struct{})}, nil
}

func (d *processVideoDecoder) close() {
	d.input.Close()
	d.cancel()
	d.output.Close()
	if d.started {
		<-d.done
	} else {
		d.command.Wait()
	}
}

func (d *processVideoDecoder) decode(data []byte, stream *ScreenStream) error {
	if !d.started {
		d.started = true
		stream.mu.Lock()
		stream.decoderLog = d.log
		stream.mu.Unlock()
		context.AfterFunc(stream.ctx, d.cancel)
		go func() {
			err := d.readFrames(stream)
			d.mu.Lock()
			d.err = err
			d.mu.Unlock()
			if err != nil && stream.ctx.Err() == nil {
				stream.mu.Lock()
				stream.err = err
				stream.mu.Unlock()
				stream.cancel()
			}
			d.command.Wait()
			close(d.done)
		}()
	}
	d.mu.Lock()
	err := d.err
	d.mu.Unlock()
	if err != nil {
		return err
	}
	if err := writeAll(d.input, data); err != nil {
		return fmt.Errorf("写入 FFmpeg 视频数据：%w；%s", err, d.log.text())
	}
	return nil
}

func (d *processVideoDecoder) readFrames(stream *ScreenStream) error {
	reader := bufio.NewReaderSize(d.output, 64<<10)
	for {
		magic, err := readPAMLine(reader)
		if err != nil {
			return fmt.Errorf("FFmpeg 视频读取失败：%w；%s", err, d.log.text())
		}
		if magic != "P7" {
			return fmt.Errorf("FFmpeg 未返回 PAM 视频帧")
		}
		width, height, depth, maxValue := 0, 0, 0, 0
		ended := false
		for i := 0; i < 20; i++ {
			line, err := readPAMLine(reader)
			if err != nil {
				return fmt.Errorf("读取视频帧信息：%w", err)
			}
			if line == "ENDHDR" {
				ended = true
				break
			}
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			value, _ := strconv.Atoi(parts[1])
			switch parts[0] {
			case "WIDTH":
				width = value
			case "HEIGHT":
				height = value
			case "DEPTH":
				depth = value
			case "MAXVAL":
				maxValue = value
			}
		}
		if !ended || width < 1 || height < 1 || width > 2048 || height > 2048 || width*height > 2_000_000 || depth != 4 || maxValue != 255 {
			return fmt.Errorf("FFmpeg 视频帧尺寸/格式无效：%d × %d × %d", width, height, depth)
		}
		pixels := stream.acquirePixels(width * height * 4)
		if _, err := io.ReadFull(reader, pixels); err != nil {
			stream.ReleaseFrame(&VideoFrame{Pixels: pixels})
			return fmt.Errorf("读取解码像素：%w", err)
		}
		stream.publish(&VideoFrame{Pixels: pixels, Size: image.Pt(width, height), RGB: true})
	}
}

func readPAMLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return "", err
	}
	if len(line) > 256 {
		return "", fmt.Errorf("视频帧头过长")
	}
	return strings.TrimSpace(string(line)), nil
}
