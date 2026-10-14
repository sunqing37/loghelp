package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"loghelp/internal/mirrorassets"
)

const mirrorVersion = "3.3.4"

type videoDecode interface {
	decode([]byte, *ScreenStream) error
	close()
}

type VideoFrame struct {
	Pixels           []byte // packed Y,U,V,255; GPU converts to RGB
	Size             image.Point
	FullRange, BT709 bool
	RGB              bool
}

type ScreenStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	control    chan []byte
	mu         sync.Mutex
	latest     *VideoFrame
	err        error
	frames     uint64
	ready      bool
	pixels     sync.Pool
	decoderLog *streamLog
}

func StartScreenStream(parent context.Context, adb *ADB, serial string) *ScreenStream {
	ctx, cancel := context.WithCancel(parent)
	s := &ScreenStream{ctx: ctx, cancel: cancel, done: make(chan struct{}), control: make(chan []byte, 64)}
	go func() {
		err := s.run(adb, serial)
		s.mu.Lock()
		if parent.Err() == nil && ctx.Err() == nil {
			s.err = err
		}
		s.ready = false
		s.mu.Unlock()
		cancel()
		close(s.done)
	}()
	return s
}

func (s *ScreenStream) Close()                { s.cancel() }
func (s *ScreenStream) Done() <-chan struct{} { return s.done }

func (s *ScreenStream) Diagnostic() string {
	s.mu.Lock()
	log := s.decoderLog
	s.mu.Unlock()
	if log != nil {
		return log.text()
	}
	return ""
}
func (s *ScreenStream) State() (bool, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready, s.frames, s.err
}
func (s *ScreenStream) TakeFrame() *VideoFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.latest
	s.latest = nil
	return f
}
func (s *ScreenStream) ReleaseFrame(f *VideoFrame) {
	if f != nil {
		s.pixels.Put(f.Pixels)
	}
}
func (s *ScreenStream) acquirePixels(size int) []byte {
	if value := s.pixels.Get(); value != nil {
		if data := value.([]byte); cap(data) >= size {
			return data[:size]
		}
	}
	return make([]byte, size)
}
func (s *ScreenStream) publish(frame *VideoFrame) {
	s.mu.Lock()
	old := s.latest
	s.latest = frame
	s.frames++
	s.mu.Unlock()
	s.ReleaseFrame(old)
}

var mirrorExtractMu sync.Mutex

func mirrorRuntimeDirectory() (string, error) {
	if custom := os.Getenv("LOGHELP_MIRROR_RUNTIME"); custom != "" {
		return filepath.Abs(custom)
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("定位投屏运行库目录：%w", err)
	}
	directory := filepath.Join(root, "loghelp", "mirror-"+mirrorVersion)
	mirrorExtractMu.Lock()
	defer mirrorExtractMu.Unlock()
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", fmt.Errorf("创建投屏运行库目录：%w", err)
	}
	for _, embedded := range [][]byte{mirrorassets.CommonArchive, mirrorassets.Archive} {
		if len(embedded) == 0 {
			continue
		}
		archive, err := zip.NewReader(bytes.NewReader(embedded), int64(len(embedded)))
		if err != nil {
			return "", fmt.Errorf("读取内嵌投屏运行库：%w", err)
		}
		for _, entry := range archive.File {
			if filepath.Base(entry.Name) != entry.Name || strings.ContainsAny(entry.Name, `/\`) || entry.UncompressedSize64 > 32<<20 {
				return "", fmt.Errorf("内嵌投屏运行库文件无效：%s", entry.Name)
			}
			reader, err := entry.Open()
			if err != nil {
				return "", err
			}
			data, readErr := io.ReadAll(io.LimitReader(reader, 32<<20+1))
			closeErr := reader.Close()
			if readErr != nil {
				return "", readErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			if len(data) > 32<<20 {
				return "", fmt.Errorf("投屏运行库文件过大")
			}
			target := filepath.Join(directory, entry.Name)
			if current, err := os.ReadFile(target); err == nil && sha256.Sum256(current) == sha256.Sum256(data) {
				continue
			}
			if err := AtomicWrite(target, data); err != nil {
				return "", fmt.Errorf("解压投屏运行库 %s：%w", entry.Name, err)
			}
		}
	}
	return directory, nil
}

type streamLog struct {
	mu     sync.Mutex
	buffer limitedBuffer
}

// Serialize shutdown with writes so a finger already pressed on the phone is
// released even if cancellation happens before the UI's queued up/cancel event.
type mirrorControlConn struct {
	conn    net.Conn
	mu      sync.Mutex
	once    sync.Once
	pressed []byte
}

func (c *mirrorControlConn) write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := writeAll(c.conn, data); err != nil {
		return err
	}
	if len(data) == 32 && data[0] == 2 {
		if data[1] == 1 || data[1] == 3 {
			c.pressed = nil
		} else {
			c.pressed = append(c.pressed[:0], data...)
		}
	}
	return nil
}

func (c *mirrorControlConn) close() {
	c.once.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.pressed) != 0 {
			c.pressed[1], c.pressed[22], c.pressed[23] = 3, 0, 0
			c.conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
			if err := writeAll(c.conn, c.pressed); err != nil {
				fmt.Fprintln(os.Stderr, "结束设备触摸：", err)
			}
			c.pressed = nil
		}
		c.conn.Close()
	})
}

func (b *streamLog) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}
func (b *streamLog) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buffer.b.String())
}

func (s *ScreenStream) run(adb *ADB, serial string) error {
	if adb == nil || strings.TrimSpace(serial) == "" {
		return fmt.Errorf("请先配置 ADB 并选择设备")
	}
	directory, err := mirrorRuntimeDirectory()
	if err != nil {
		return err
	}
	decoder, err := newVideoDecoder(directory)
	if err != nil {
		return err
	}
	defer decoder.close()
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	id := fmt.Sprintf("%08x", binary.BigEndian.Uint32(random[:])&0x7fffffff)
	remote := "/data/local/tmp/loghelp-scrcpy-" + id + ".jar"
	setup, setupCancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer setupCancel()
	if _, err := adb.Run(setup, serial, "push", filepath.Join(directory, "scrcpy-server"), remote); err != nil {
		return fmt.Errorf("安装临时投屏服务：%w", err)
	}
	// Only this session's temporary file and forwarding rule are removed.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		adb.Run(ctx, serial, "shell", "rm", "-f", remote)
	}()
	portText, err := adb.Run(setup, serial, "forward", "tcp:0", "localabstract:scrcpy_"+id)
	if err != nil {
		return fmt.Errorf("创建投屏连接：%w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("投屏转发端口无效：%q", portText)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		adb.Run(ctx, serial, "forward", "--remove", "tcp:"+strconv.Itoa(port))
	}()
	serverContext, serverCancel := context.WithCancel(s.ctx)
	defer serverCancel()
	command := adb.Command(serverContext, serial, "shell", "CLASSPATH="+remote, "app_process", "/", "com.genymobile.scrcpy.Server", mirrorVersion,
		"scid="+id, "tunnel_forward=true", "audio=false", "control=true", "max_size=1280", "max_fps=60", "video_bit_rate=8000000", "send_device_meta=false", "clipboard_autosync=false", "power_on=false")
	log := &streamLog{buffer: limitedBuffer{limit: 16 << 10}}
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动投屏服务：%w", err)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- command.Wait() }()
	defer func() { serverCancel(); <-serverDone }()
	adbAddress, err := serverAddress()
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(adbAddress)
	if err != nil {
		return fmt.Errorf("ADB 服务地址无效：%w", err)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	video, err := connectMirror(setup, address)
	if err != nil {
		return fmt.Errorf("连接投屏服务：%w；%s", err, log.text())
	}
	defer video.Close()
	stopVideo := context.AfterFunc(s.ctx, func() { video.Close() })
	defer stopVideo()
	control, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(setup, "tcp", address)
	if err != nil {
		return fmt.Errorf("连接投屏控制：%w", err)
	}
	controlConnection := &mirrorControlConn{conn: control}
	defer controlConnection.close()
	stopControl := context.AfterFunc(s.ctx, controlConnection.close)
	defer stopControl()
	video.SetReadDeadline(time.Now().Add(10 * time.Second))
	var metadata [12]byte
	if _, err := io.ReadFull(video, metadata[:]); err != nil {
		return fmt.Errorf("读取投屏信息：%w；%s", err, log.text())
	}
	if binary.BigEndian.Uint32(metadata[:4]) != 0x68323634 {
		return fmt.Errorf("设备未返回 H.264 视频")
	}
	video.SetReadDeadline(time.Time{})
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	controlErrors := make(chan error, 1)
	go s.writeControls(controlConnection, controlErrors)
	// Consume server messages even though clipboard text synchronization is off.
	go io.Copy(io.Discard, control)
	var header [12]byte
	var config, packet []byte
	for {
		if _, err := io.ReadFull(video, header[:]); err != nil {
			select {
			case controlErr := <-controlErrors:
				return controlErr
			default:
			}
			return fmt.Errorf("投屏连接已断开：%w；%s", err, log.text())
		}
		size := int(binary.BigEndian.Uint32(header[8:]))
		if size < 1 || size > 8<<20 {
			return fmt.Errorf("视频数据包长度无效：%d", size)
		}
		if cap(packet) < size {
			packet = make([]byte, size)
		} else {
			packet = packet[:size]
		}
		if _, err := io.ReadFull(video, packet); err != nil {
			return fmt.Errorf("读取视频数据：%w", err)
		}
		if header[0]&0x80 != 0 {
			if size > 1<<20 {
				return fmt.Errorf("视频配置数据过大")
			}
			config = append(config[:0], packet...)
			continue
		}
		payload := packet
		if len(config) > 0 {
			config = append(config, packet...)
			payload = config
		}
		if err := decoder.decode(payload, s); err != nil {
			return err
		}
		config = config[:0]
	}
}

func connectMirror(ctx context.Context, address string) (net.Conn, error) {
	var last error
	for ctx.Err() == nil {
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
		if err == nil {
			conn.SetReadDeadline(time.Now().Add(time.Second))
			var dummy [1]byte
			_, err = io.ReadFull(conn, dummy[:])
			if err == nil && dummy[0] == 0 {
				conn.SetReadDeadline(time.Time{})
				return conn, nil
			}
			conn.Close()
		}
		last = err
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%w；%v", ctx.Err(), last)
		case <-timer.C:
		}
	}
	return nil, ctx.Err()
}

func (s *ScreenStream) writeControls(conn *mirrorControlConn, errors chan<- error) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case data := <-s.control:
			if err := conn.write(data); err != nil {
				if s.ctx.Err() != nil {
					return
				}
				errors <- fmt.Errorf("投屏控制连接失败：%w", err)
				s.mu.Lock()
				s.err = fmt.Errorf("投屏控制连接失败：%w", err)
				s.mu.Unlock()
				s.cancel()
				return
			}
		}
	}
}

func (s *ScreenStream) sendControl(data []byte) error {
	ready, _, err := s.State()
	if err != nil {
		return err
	}
	if !ready || s.ctx.Err() != nil {
		return fmt.Errorf("投屏尚未连接，请稍后重试")
	}
	select {
	case s.control <- data:
		return nil
	default:
		return fmt.Errorf("投屏控制繁忙，请重新连接")
	}
}

func (s *ScreenStream) Touch(action byte, point, size image.Point) error {
	if action > 3 || size.X < 1 || size.Y < 1 || size.X > 65535 || size.Y > 65535 || point.X < 0 || point.Y < 0 || point.X >= size.X || point.Y >= size.Y {
		return fmt.Errorf("投屏触摸坐标无效")
	}
	data := make([]byte, 32)
	data[0], data[1] = 2, action
	binary.BigEndian.PutUint64(data[2:10], ^uint64(1)) // generic finger (-2)
	binary.BigEndian.PutUint32(data[10:14], uint32(point.X))
	binary.BigEndian.PutUint32(data[14:18], uint32(point.Y))
	binary.BigEndian.PutUint16(data[18:20], uint16(size.X))
	binary.BigEndian.PutUint16(data[20:22], uint16(size.Y))
	if action != 1 && action != 3 {
		binary.BigEndian.PutUint16(data[22:24], 65535)
	}
	return s.sendControl(data)
}

func (s *ScreenStream) Key(code uint32) error {
	data := make([]byte, 28)
	for i := 0; i < 2; i++ {
		offset := i * 14
		data[offset+1] = byte(i)
		binary.BigEndian.PutUint32(data[offset+2:], code)
	}
	return s.sendControl(data)
}
