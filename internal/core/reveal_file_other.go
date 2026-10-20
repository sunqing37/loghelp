//go:build !windows

package core

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

func revealFileNative(ctx context.Context, path string) error {
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "open", "-R", path).Run()
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	var selectErr error
	if command, err := exec.LookPath("gdbus"); err == nil {
		selectErr = exec.CommandContext(ctx, command, "call", "--session", "--dest", "org.freedesktop.FileManager1", "--object-path", "/org/freedesktop/FileManager1", "--method", "org.freedesktop.FileManager1.ShowItems", "["+strconv.Quote(uri)+"]", "").Run()
		if selectErr == nil {
			return nil
		}
	} else if command, err := exec.LookPath("dbus-send"); err == nil {
		selectErr = exec.CommandContext(ctx, command, "--session", "--type=method_call", "--print-reply", "--dest=org.freedesktop.FileManager1", "/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:"+uri, "string:").Run()
		if selectErr == nil {
			return nil
		}
	}
	// Desktop environments without FileManager1 can still open the containing
	// directory; the UI keeps the exact filename visible for quick location.
	if err := exec.CommandContext(ctx, "xdg-open", filepath.Dir(path)).Run(); err != nil {
		return fmt.Errorf("打开文件夹：%w；文件选择服务：%v", err, selectErr)
	}
	return nil
}
