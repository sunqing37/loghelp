package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DeviceFile uses Android (POSIX) mode bits, independent of the host OS.
type DeviceFile struct {
	Name     string
	Mode     uint32
	Size     int64
	Modified time.Time
}

func (f DeviceFile) IsDir() bool     { return f.Mode&0170000 == 0040000 }
func (f DeviceFile) IsLink() bool    { return f.Mode&0170000 == 0120000 }
func (f DeviceFile) IsRegular() bool { return f.Mode&0170000 == 0100000 }

func deviceFileQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func deviceFilePath(serial, value string) (string, error) {
	if serial == "" {
		return "", fmt.Errorf("请选择在线设备")
	}
	if !strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("请输入以 / 开头的设备绝对路径")
	}
	return path.Clean(value), nil
}

// ListDeviceFiles uses the binary sync-v2 directory stream, avoiding one
// Android stat process per entry. Older daemons retain the shell fallback.
func (a *ADB) ListDeviceFiles(ctx context.Context, serial, directory string) ([]DeviceFile, error) {
	directory, err := deviceFilePath(serial, directory)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("请先配置 ADB")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	entries, fastErr := listDeviceFilesSync(ctx, serial, directory)
	if fastErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("读取目录 %s：%w", directory, ctx.Err())
		}
		entries, err = a.listDeviceFilesShell(ctx, serial, directory)
		if err != nil {
			return nil, errors.Join(fastErr, err)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// NUL-terminated names preserve spaces, quotes, Unicode and newlines. Metadata
// is separate from names; unlike parsing ls output this cannot shift columns.
func (a *ADB) listDeviceFilesShell(ctx context.Context, serial, directory string) ([]DeviceFile, error) {
	command := "cd " + deviceFileQuote(directory) + ` && { ls -a . >/dev/null || exit; n=0
for f in .[!.]* ..?* *; do
  [ -e "$f" ] || [ -L "$f" ] || continue
  n=$((n+1)); [ "$n" -le 10000 ] || { echo '目录超过 10000 项，请进入更具体的目录' >&2; exit 1; }
  stat -c '%f %s %Y' -- "$f" || exit
  printf '%s\000' "$f"
done
}`
	if a == nil {
		return nil, fmt.Errorf("请先配置 ADB")
	}
	cmd := a.Command(ctx, serial, "shell", "-T", command)
	out, diagnostic := &limitedBuffer{limit: 8 << 20}, &limitedBuffer{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	if err := cmd.Run(); err != nil {
		detail := diagnostic.b.String()
		lower := strings.ToLower(detail)
		if strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted") {
			return nil, fmt.Errorf("无访问权限：%s：%w：%s", directory, os.ErrPermission, detail)
		}
		return nil, fmt.Errorf("读取目录 %s 失败（请检查权限和设备连接）：%w：%s", directory, err, detail)
	}
	if out.b.Len() >= out.limit {
		return nil, fmt.Errorf("目录内容过大，请进入更具体的目录")
	}
	var entries []DeviceFile
	remaining := out.b.String()
	for remaining != "" {
		metadata, rest, ok := strings.Cut(remaining, "\n")
		if !ok {
			return nil, fmt.Errorf("目录元数据不完整")
		}
		name, rest, ok := strings.Cut(rest, "\x00")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return nil, fmt.Errorf("设备返回了无效目录项")
		}
		mode, e1 := strconv.ParseUint(fields[0], 16, 32)
		size, e2 := strconv.ParseInt(fields[1], 10, 64)
		modified, e3 := strconv.ParseInt(fields[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || size < 0 {
			return nil, fmt.Errorf("无法解析文件 %q 的属性", name)
		}
		entries = append(entries, DeviceFile{Name: name, Mode: uint32(mode), Size: size, Modified: time.Unix(modified, 0)})
		remaining = rest
	}
	return entries, nil
}

// Stage beside the destination. A failed/cancelled pull never truncates the
// user's existing file; rename only after ADB acknowledges successful delivery.
func (a *ADB) DownloadDeviceFile(ctx context.Context, serial, remote, local string) (resultErr error) {
	remote, err := deviceFilePath(serial, remote)
	if err != nil {
		return err
	}
	if _, err = a.Run(ctx, serial, "shell", "-T", "[ -f "+deviceFileQuote(remote)+" ] && [ ! -L "+deviceFileQuote(remote)+" ]"); err != nil {
		return fmt.Errorf("只能下载可读取的普通文件，请刷新目录：%w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(local), ".loghelp-download-*")
	if err != nil {
		return fmt.Errorf("创建本地下载临时目录：%w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(stage)) }()
	temporary := filepath.Join(stage, "download")
	if _, err = a.Run(ctx, serial, "pull", remote, temporary); err != nil {
		return fmt.Errorf("下载 %s：%w", remote, err)
	}
	info, err := os.Lstat(temporary)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("下载结果不是普通文件，请刷新目录后重试：%v", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(temporary, local); err != nil {
		return fmt.Errorf("保存下载文件 %s：%w", local, err)
	}
	return nil
}

// Reuse the binary Shell-v2 upload transport: piping binary data into the
// Windows adb shell CLI can translate CR/LF and treat 0x1a as end-of-file.
func (a *ADB) UploadDeviceFile(ctx context.Context, serial, local, remote string, progress func(sent, total int64)) (resultErr error) {
	remote, err := deviceFilePath(serial, remote)
	if err != nil {
		return err
	}
	info, err := os.Stat(local)
	if err != nil {
		return fmt.Errorf("读取上传文件：%w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("请选择普通文件上传")
	}
	// Enter the parent first so mktemp's response never contains a user-supplied
	// newline; the random basename is also validated before cleanup uses it.
	parent := deviceFileQuote(path.Dir(remote))
	name, err := a.Run(ctx, serial, "shell", "-T", "cd "+parent+" && mktemp .loghelp-upload-XXXXXXXX")
	if err != nil {
		return fmt.Errorf("无法在目标目录创建文件（请检查写权限）：%w", err)
	}
	if !strings.HasPrefix(name, ".loghelp-upload-") || strings.ContainsAny(name, "/\r\n\x00") {
		return fmt.Errorf("设备返回了无效的临时文件名：%q", name)
	}
	temporary := path.Join(path.Dir(remote), name)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := a.Run(cleanup, serial, "shell", "-T", "rm -f -- "+deviceFileQuote(temporary)); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("清理设备临时文件 %s 失败：%w", temporary, err))
		}
	}()
	command := "cat > " + deviceFileQuote(temporary) + " && [ \"$(stat -c %s -- " + deviceFileQuote(temporary) + ")\" = " + strconv.FormatInt(info.Size(), 10) + " ]"
	_, err = a.runInstallUpload(ctx, serial, local, info.Size(), command, func(n int64) {
		if progress != nil {
			progress(n, info.Size())
		}
	})
	if err != nil {
		return fmt.Errorf("上传 %s：%w", filepath.Base(local), err)
	}
	// -T prevents an existing directory/symlink-to-directory from silently
	// changing the destination. A symlink to a file is replaced, never followed.
	if _, err = a.Run(ctx, serial, "shell", "-T", "mv -fT -- "+deviceFileQuote(temporary)+" "+deviceFileQuote(remote)); err != nil {
		return fmt.Errorf("保存设备文件 %s 失败：%w", remote, err)
	}
	return nil
}
