package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Shell-v2 is the ADB protocol itself, not a host shell wrapper. This gives
// Windows and Unix the same remote PTY, including signals and resize events.
type Shell struct {
	conn      net.Conn
	writeMu   sync.Mutex
	readMu    sync.Mutex
	remaining []byte
	closed    sync.Once
	ExitCode  int
}

func serverAddress() (string, error) {
	if value := os.Getenv("ADB_SERVER_SOCKET"); value != "" {
		if !strings.HasPrefix(value, "tcp:") {
			return "", fmt.Errorf("暂不支持 ADB_SERVER_SOCKET=%s，请使用 TCP ADB 服务", value)
		}
		value = strings.TrimPrefix(value, "tcp:")
		if !strings.Contains(value, ":") {
			value = "127.0.0.1:" + value
		}
		return value, nil
	}
	host := os.Getenv("ANDROID_ADB_SERVER_ADDRESS")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("ANDROID_ADB_SERVER_PORT")
	if port == "" {
		port = "5037"
	}
	return net.JoinHostPort(host, port), nil
}
func adbRequest(conn net.Conn, service string) error {
	if len(service) > 65535 {
		return fmt.Errorf("ADB 请求过长")
	}
	if err := writeAll(conn, []byte(fmt.Sprintf("%04x%s", len(service), service))); err != nil {
		return err
	}
	status := make([]byte, 4)
	if _, err := io.ReadFull(conn, status); err != nil {
		return err
	}
	if string(status) == "OKAY" {
		return nil
	}
	if string(status) != "FAIL" {
		return fmt.Errorf("无效 ADB 响应 %q", status)
	}
	if _, err := io.ReadFull(conn, status); err != nil {
		return err
	}
	n, err := strconv.ParseUint(string(status), 16, 16)
	if err != nil {
		return err
	}
	message := make([]byte, n)
	if _, err := io.ReadFull(conn, message); err != nil {
		return err
	}
	return fmt.Errorf("ADB：%s", message)
}
func OpenShell(ctx context.Context, adb *ADB, serial string) (*Shell, error) {
	if adb == nil || serial == "" {
		return nil, fmt.Errorf("请选择在线设备并配置 ADB")
	}
	// Devices is non-destructive and starts the configured server if needed.
	if _, err := adb.Devices(ctx); err != nil {
		return nil, err
	}
	address, err := serverAddress()
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err = adbRequest(conn, "host:transport:"+serial); err == nil {
		err = adbRequest(conn, "shell,v2,pty,TERM=xterm-256color:")
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("设备不支持交互 Shell-v2 或连接失败：%w", err)
	}
	conn.SetDeadline(time.Time{})
	return &Shell{conn: conn, ExitCode: -1}, nil
}
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
func (s *Shell) packet(id byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer s.conn.SetWriteDeadline(time.Time{})
	header := make([]byte, 5)
	header[0] = id
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))
	if err := writeAll(s.conn, header); err != nil {
		return err
	}
	return writeAll(s.conn, payload)
}
func (s *Shell) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > 32768 {
			n = 32768
		}
		if err := s.packet(0, p[:n]); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}
func (s *Shell) Read(p []byte) (int, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	for len(s.remaining) == 0 {
		header := make([]byte, 5)
		if _, err := io.ReadFull(s.conn, header); err != nil {
			return 0, err
		}
		n := binary.LittleEndian.Uint32(header[1:])
		if n > 16<<20 {
			return 0, fmt.Errorf("异常 Shell 帧大小 %d", n)
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(s.conn, payload); err != nil {
			return 0, err
		}
		switch header[0] {
		case 1, 2:
			s.remaining = payload
		case 3:
			if len(payload) > 0 {
				s.ExitCode = int(payload[0])
			}
			return 0, io.EOF
		default:
		}
	}
	n := copy(p, s.remaining)
	s.remaining = s.remaining[n:]
	return n, nil
}
func (s *Shell) Resize(rows, cols uint) error {
	if rows == 0 || cols == 0 {
		return nil
	}
	return s.packet(5, []byte(fmt.Sprintf("%dx%d,0x0\x00", rows, cols)))
}
func (s *Shell) Close() error {
	var err error
	s.closed.Do(func() { err = s.conn.Close() })
	return err
}
