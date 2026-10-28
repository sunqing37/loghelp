//go:build darwin || linux

package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"loghelp/internal/core"
)

func sharedClipboardCommand(write bool) (string, []string, error) {
	if runtime.GOOS == "darwin" {
		if write {
			return "/usr/bin/pbcopy", nil, nil
		}
		return "/usr/bin/pbpaste", []string{"-Prefer", "txt"}, nil
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		name, args := "wl-paste", []string{"--no-newline", "--type", "text"}
		if write {
			name, args = "wl-copy", []string{"--type", "text/plain;charset=utf-8"}
		}
		path, err := exec.LookPath(name)
		if err != nil {
			return "", nil, fmt.Errorf("Wayland 文本共享需要安装 wl-clipboard")
		}
		return path, args, nil
	}
	if os.Getenv("DISPLAY") != "" {
		path, err := exec.LookPath("xclip")
		if err == nil {
			mode := "-o"
			if write {
				mode = "-i"
			}
			return path, []string{"-selection", "clipboard", "-target", "UTF8_STRING", mode}, nil
		}
	}
	return "", nil, fmt.Errorf("X11 文本共享需要安装 xclip，并在图形桌面中运行")
}

type clipboardOutput struct{ bytes.Buffer }

func (b *clipboardOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > core.ScreenClipboardTextLimit {
		return 0, fmt.Errorf("剪贴板文本过长，未同步")
	}
	return b.Buffer.Write(p)
}

func clipboardProcess(ctx context.Context, write bool, text string, formats bool) (string, error) {
	name, args, err := sharedClipboardCommand(write)
	if err != nil {
		return "", err
	}
	if formats {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			args = []string{"--list-types"}
		} else {
			args = []string{"-selection", "clipboard", "-target", "TARGETS", "-o"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	if runtime.GOOS == "darwin" {
		command.Env = append(os.Environ(), "LC_CTYPE=en_US.UTF-8")
	}
	// Bound subprocess pipes even when an external clipboard owner hangs.
	command.WaitDelay = 200 * time.Millisecond
	var output clipboardOutput
	if write {
		command.Stdin = strings.NewReader(text)
	} else {
		command.Stdout = &output
	}
	// No captured stderr: clipboard owners may fork, and diagnostics may
	// contain copied data. Report only the command failure.
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%s 执行失败（需可用的文本剪贴板）：%w", name, err)
	}
	return output.String(), nil
}

func readSharedClipboard(ctx context.Context) (string, bool, error) {
	if runtime.GOOS == "linux" {
		formats, err := clipboardProcess(ctx, false, "", true)
		if err != nil {
			return "", false, err
		}
		textAvailable := false
		for _, format := range strings.Fields(formats) {
			if os.Getenv("WAYLAND_DISPLAY") != "" {
				textAvailable = textAvailable || strings.HasPrefix(format, "text/")
			} else {
				textAvailable = textAvailable || format == "UTF8_STRING"
			}
		}
		if !textAvailable {
			return "", false, nil
		}
	}
	text, err := clipboardProcess(ctx, false, "", false)
	return text, err == nil && text != "", err
}

func writeSharedClipboard(ctx context.Context, text string) error {
	_, err := clipboardProcess(ctx, true, text, false)
	return err
}
