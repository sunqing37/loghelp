package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"loghelp/internal/core"
)

func copyScreenshotToClipboard(frame *core.ScreenFrame) error {
	if frame == nil || len(frame.PNG) == 0 {
		return fmt.Errorf("没有可复制的截图")
	}
	var command string
	var args []string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if path, err := exec.LookPath("wl-copy"); err == nil {
			command, args = path, []string{"--type", "image/png"}
		}
	}
	if command == "" && os.Getenv("DISPLAY") != "" {
		if path, err := exec.LookPath("xclip"); err == nil {
			command, args = path, []string{"-selection", "clipboard", "-t", "image/png", "-i"}
		}
	}
	if command == "" {
		return fmt.Errorf("图片剪贴板不可用：Wayland 请安装 wl-clipboard，X11 请安装 xclip，并在图形桌面中运行")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = bytes.NewReader(frame.PNG)
	// Clipboard managers fork to keep owning the selection. Do not leave a
	// captured stderr pipe open in that child or Wait would block indefinitely.
	log, err := os.CreateTemp("", "loghelp-clipboard-*.txt")
	if err != nil {
		return fmt.Errorf("创建剪贴板诊断文件：%w", err)
	}
	defer os.Remove(log.Name())
	defer log.Close()
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		log.Seek(0, 0)
		var output [4096]byte
		n, _ := log.Read(output[:])
		return fmt.Errorf("复制截图到图片剪贴板：%w；%s", err, strings.TrimSpace(string(output[:n])))
	}
	return nil
}
