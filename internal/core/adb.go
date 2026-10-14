package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ADB struct{ Path, Version string }
type Device struct{ Serial, Name, State, Connection string }
type Process struct {
	PID           int
	Name, Package string
	FirstSeen     time.Time
}

func DiscoverADB(configured string) (*ADB, error) {
	path := strings.TrimSpace(configured)
	if path == "" {
		var err error
		path, err = exec.LookPath("adb")
		if err != nil {
			return nil, fmt.Errorf("PATH 中没有 ADB，请配置 adb 可执行文件路径")
		}
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		name := "adb"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path = filepath.Join(path, name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	a := &ADB{Path: abs}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	version, err := a.Run(ctx, "", "version")
	if err != nil {
		return nil, fmt.Errorf("ADB 路径无效 %s：%w", abs, err)
	}
	if !strings.Contains(version, "Android Debug Bridge") {
		return nil, fmt.Errorf("所选文件不是 ADB：%s", abs)
	}
	a.Version = strings.TrimSpace(version)
	return a, nil
}
func (a *ADB) Command(ctx context.Context, serial string, args ...string) *exec.Cmd {
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	cmd := exec.CommandContext(ctx, a.Path, args...)
	configureProcess(cmd)
	return cmd
}

type limitedBuffer struct {
	b     bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	space := b.limit - b.b.Len()
	if space > 0 {
		if len(p) > space {
			p = p[:space]
		}
		b.b.Write(p)
	}
	return n, nil
}
func (a *ADB) Run(ctx context.Context, serial string, args ...string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("请先配置 ADB")
	}
	cmd := a.Command(ctx, serial, args...)
	out := &limitedBuffer{limit: 4 << 20}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	text := strings.TrimSpace(out.b.String())
	if err != nil {
		return text, fmt.Errorf("%w：%s", err, text)
	}
	return text, nil
}
func (a *ADB) Devices(ctx context.Context) ([]Device, error) {
	text, err := a.Run(ctx, "", "devices", "-l")
	if err != nil {
		return nil, err
	}
	var devices []Device
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == "List" || strings.HasPrefix(f[0], "*") {
			continue
		}
		d := Device{Serial: f[0], Name: f[0], State: f[1], Connection: "USB"}
		if strings.Contains(f[0], ":") {
			d.Connection = "网络"
		}
		for _, v := range f[2:] {
			if strings.HasPrefix(v, "model:") {
				d.Name = strings.ReplaceAll(strings.TrimPrefix(v, "model:"), "_", " ")
			}
		}
		devices = append(devices, d)
	}
	return devices, nil
}
func (a *ADB) Packages(ctx context.Context, serial string) ([]string, error) {
	out, err := a.Run(ctx, serial, "shell", "cmd", "package", "list", "packages")
	if err != nil {
		return nil, err
	}
	var packages []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "package:") {
			packages = append(packages, strings.TrimSpace(strings.TrimPrefix(line, "package:")))
		}
	}
	return packages, nil
}
func (a *ADB) Processes(ctx context.Context, serial string) (map[int]Process, error) {
	out, err := a.Run(ctx, serial, "shell", "ps", "-A", "-o", "PID,NAME,ARGS")
	if err != nil {
		return nil, err
	}
	result := map[int]Process{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		name := f[1]
		if len(f) > 2 && strings.Contains(f[2], ".") {
			name = f[2]
		}
		// Entries retain process metadata after the next poll. A substring of
		// out would keep the entire ps response alive for every retained poll.
		name = strings.Clone(name)
		pkg := strings.Split(name, ":")[0]
		if !strings.Contains(pkg, ".") || strings.HasPrefix(pkg, "/") {
			pkg = ""
		}
		result[pid] = Process{PID: pid, Name: name, Package: pkg}
	}
	return result, nil
}
func BufferArgs(buffers []string) []string {
	var args []string
	for _, b := range buffers {
		args = append(args, "-b", b)
	}
	return args
}
func ParseBuffers(text string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, s := range strings.Split(text, ",") {
		s = strings.TrimSpace(s)
		switch s {
		case "main", "system", "crash", "events", "radio", "kernel":
		default:
			return nil, fmt.Errorf("不支持的缓冲区 %q", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("至少选择一个缓冲区")
	}
	return out, nil
}
func (a *ADB) CheckBuffers(ctx context.Context, serial string, buffers []string) error {
	_, err := a.Run(ctx, serial, append([]string{"logcat", "-g"}, BufferArgs(buffers)...)...)
	return err
}
func (a *ADB) Clear(ctx context.Context, serial string, buffers []string) error {
	_, err := a.Run(ctx, serial, append([]string{"logcat", "-c"}, BufferArgs(buffers)...)...)
	return err
}

func readLines(r io.Reader, consume func(string)) error {
	// bufio.Reader.ReadString preserves records larger than Scanner's token limit.
	return readStreamLines(r, consume)
}
