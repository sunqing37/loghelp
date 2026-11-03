package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var installSessionID = regexp.MustCompile(`(?m)^Success: created install session \[([0-9]+)\]\r?$`)

func hasInstallFeature(features, wanted string) bool {
	for _, feature := range strings.FieldsFunc(features, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' }) {
		if strings.TrimSpace(feature) == wanted {
			return true
		}
	}
	return false
}

func (a *ADB) installLegacyAPKs(ctx context.Context, serial string, apks []installAPK, progress func(stage, file string, sent, total int64)) error {
	progress("此设备或 ADB 不支持字节进度，正在安装；请留意手机安装确认", "", 0, 0)
	args := []string{"install", "-r"}
	if len(apks) > 1 {
		args[0] = "install-multiple"
	}
	for _, apk := range apks {
		args = append(args, apk.path)
	}
	out, err := a.Run(ctx, serial, args...)
	if err != nil {
		return err
	}
	if !installOutputSucceeded(out) {
		return fmt.Errorf("设备未返回安装成功：%s", out)
	}
	return nil
}

// Each APK is streamed into one PackageInstaller session, preserving atomic
// base/split installation and -r semantics. Counts measure bytes written into
// the binary Shell-v2 transport; only the device's final Success means installed.
func (a *ADB) installMeasuredAPKs(ctx context.Context, serial string, apks []installAPK, progress func(stage, file string, sent, total int64)) (resultErr error) {
	sizes := make([]int64, len(apks))
	var total int64
	for i, apk := range apks {
		info, err := os.Stat(apk.path)
		if err != nil {
			return fmt.Errorf("读取 APK %s：%w", apk.name, err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > (1<<63-1)-total {
			return fmt.Errorf("APK 大小无效：%s", apk.name)
		}
		sizes[i] = info.Size()
		total += sizes[i]
	}
	progress("正在创建设备安装会话", "", 0, total)
	out, err := a.Run(ctx, serial, "shell", "pm", "install-create", "-r", "-S", strconv.FormatInt(total, 10))
	if err != nil {
		return fmt.Errorf("创建设备安装会话：%w", err)
	}
	match := installSessionID.FindStringSubmatch(strings.TrimSpace(out))
	if len(match) != 2 {
		return fmt.Errorf("设备未返回安装会话编号：%s", out)
	}
	id := match[1]
	committed := false
	defer func() {
		if committed {
			return
		}
		// Cancellation must also release the device's staged APK files. It must
		// not reuse the cancelled context or abandon a different installation.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := a.Run(cleanupCtx, serial, "shell", "pm", "install-abandon", id)
		if err == nil && !installOutputSucceeded(out) {
			err = fmt.Errorf("%s", out)
		}
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("清理安装会话 %s 失败，请检查设备：%w", id, err))
		}
	}()
	var sent int64
	for i, apk := range apks {
		name := fmt.Sprintf("split-%d.apk", i)
		if i == 0 {
			name = "base.apk"
		}
		command := fmt.Sprintf("pm install-write -S %d %s %s -", sizes[i], id, name)
		out, err = a.runInstallUpload(ctx, serial, apk.path, sizes[i], command, func(n int64) {
			stage := "正在发送 APK"
			if n == sizes[i] {
				stage = "等待设备确认 APK"
			}
			progress(stage, apk.name, sent+n, total)
		})
		if err != nil {
			return fmt.Errorf("发送 APK %s：%w", apk.name, err)
		}
		acknowledged := installOutputSucceeded(out)
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == fmt.Sprintf("Success: streamed %d bytes", sizes[i]) {
				acknowledged = true
			}
		}
		if !acknowledged {
			return fmt.Errorf("设备未确认 APK %s 写入成功：%s", apk.name, out)
		}
		sent += sizes[i]
	}
	progress("传输完成，设备安装中；请留意手机安装确认", "", total, total)
	out, err = a.Run(ctx, serial, "shell", "pm", "install-commit", id)
	if err != nil {
		return fmt.Errorf("设备安装失败：%w", err)
	}
	if !installOutputSucceeded(out) {
		return fmt.Errorf("设备未返回安装成功：%s", out)
	}
	committed = true
	return nil
}

type installUploadWriter struct {
	w           io.Writer
	sent, total int64
	last        time.Time
	progress    func(int64)
}

func (w *installUploadWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.sent += int64(n)
	if time.Since(w.last) >= 100*time.Millisecond || w.sent == w.total || err != nil {
		w.progress(w.sent)
		w.last = time.Now()
	}
	return n, err
}

// Do not feed binary files to `adb shell` through a host stdin pipe: Windows
// ADB treats 0x1a as EOF and may translate line endings even with -T. Shell-v2
// DATA/CLOSE_STDIN packets bypass that text conversion on every host platform.
func (a *ADB) runInstallUpload(ctx context.Context, serial, file string, size int64, command string, progress func(int64)) (string, error) {
	input, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return "", fmt.Errorf("安装文件在准备后发生变化，请重新选择")
	}
	stream, stop, err := openInstallUploadShell(ctx, serial, command)
	if err != nil {
		return "", err
	}
	defer stop()
	defer stream.Close()
	out := &limitedBuffer{limit: 4 << 20}
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(out, stream)
		// An early remote exit also unblocks any pending write; never continue
		// counting bytes after the receiver stopped accepting the stream.
		stream.Close()
		readDone <- readErr
	}()
	progress(0)
	writer := &installUploadWriter{w: stream, total: size, last: time.Now(), progress: progress}
	n, copyErr := io.CopyBuffer(writer, io.LimitReader(input, size), make([]byte, 64<<10))
	if n != size {
		progress(n)
	}
	if copyErr == nil && n != size {
		copyErr = fmt.Errorf("文件提前结束：已发送 %d/%d 字节", n, size)
	}
	if copyErr == nil {
		var extra [1]byte
		if n, readErr := input.Read(extra[:]); n != 0 || readErr != io.EOF {
			copyErr = fmt.Errorf("安装文件在发送时发生变化或读取失败：%v", readErr)
		}
	}
	var closeErr error
	if copyErr != nil {
		stream.Close()
	} else {
		closeErr = stream.packet(4, nil) // Shell-v2 CLOSE_STDIN, keep stdout open.
		if closeErr != nil {
			// The remote may already have exited after reading the exact size.
			// Drain its buffered exit frame instead of discarding it with Close.
			stream.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		}
	}
	readErr := <-readDone
	value := strings.TrimSpace(out.b.String())
	var exitErr error
	if stream.ExitCode != 0 {
		exitErr = fmt.Errorf("设备命令退出状态 %d（-1 表示连接未返回退出状态）", stream.ExitCode)
	}
	// A command reading a declared byte count may send its successful exit
	// before CLOSE_STDIN. Ignore only that redundant close after full delivery.
	if copyErr == nil && readErr == nil && stream.ExitCode == 0 {
		closeErr = nil
	}
	if err := errors.Join(ctx.Err(), copyErr, closeErr, readErr, exitErr); err != nil {
		return value, fmt.Errorf("%w：%s", err, value)
	}
	return value, nil
}

func openInstallUploadShell(ctx context.Context, serial, command string) (*Shell, func() bool, error) {
	network, address := "tcp", ""
	var err error
	if socket := os.Getenv("ADB_SERVER_SOCKET"); strings.HasPrefix(socket, "localfilesystem:") {
		network, address = "unix", strings.TrimPrefix(socket, "localfilesystem:")
	} else {
		address, err = serverAddress()
	}
	if err != nil {
		return nil, nil, err
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 ADB 二进制传输通道：%w", err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err = adbRequest(conn, "host:transport:"+serial); err == nil {
		err = adbRequest(conn, "shell,v2,raw:"+command)
	}
	if err != nil {
		stop()
		conn.Close()
		return nil, nil, fmt.Errorf("打开设备二进制传输通道：%w", err)
	}
	conn.SetDeadline(time.Time{})
	return &Shell{conn: conn, ExitCode: -1, writeTimeout: 30 * time.Second}, stop, nil
}

func (a *ADB) installMeasuredOBB(ctx context.Context, serial, local, remote string, progress func(stage string, sent, total int64)) error {
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	// The package and OBB names have already been validated. Quote nevertheless
	// because this one command uses remote shell redirection, unlike pm argv.
	quoted := "'" + strings.ReplaceAll(remote, "'", "'\"'\"'") + "'"
	out, err := a.runInstallUpload(ctx, serial, local, info.Size(), "cat > "+quoted+" && echo Success", func(n int64) {
		stage := "正在发送 OBB"
		if n == info.Size() {
			stage = "等待设备确认 OBB 写入"
		}
		progress(stage, n, info.Size())
	})
	if err != nil {
		return err
	}
	if !installOutputSucceeded(out) {
		return fmt.Errorf("设备未确认 OBB 写入成功：%s", out)
	}
	return nil
}
