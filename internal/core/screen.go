package core

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"time"
)

// ScreenFrame retains the original PNG for saving without display scaling.
type ScreenFrame struct {
	PNG        []byte
	Image      image.Image
	CapturedAt time.Time
}

func (a *ADB) CaptureScreen(ctx context.Context, serial string) (*ScreenFrame, error) {
	if a == nil || strings.TrimSpace(serial) == "" {
		return nil, fmt.Errorf("请先配置 ADB 并选择设备")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := a.Command(ctx, serial, "exec-out", "screencap", "-p")
	const limit = 32 << 20
	out, stderr := &limitedBuffer{limit: limit + 1}, &limitedBuffer{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("设备 %s 截图失败：%w；%s", serial, err, strings.TrimSpace(stderr.b.String()))
	}
	data := out.b.Bytes()
	if len(data) > limit {
		return nil, fmt.Errorf("设备 %s 截图超过 32 MiB", serial)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("设备 %s 未返回有效 PNG：%w；%s", serial, err, strings.TrimSpace(stderr.b.String()))
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 20_000_000 {
		return nil, fmt.Errorf("设备 %s 截图尺寸不支持：%d × %d", serial, config.Width, config.Height)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 20_000_000 {
		return nil, fmt.Errorf("设备 %s 截图尺寸不支持：%d × %d", serial, config.Width, config.Height)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("设备 %s 截图解码失败：%w", serial, err)
	}
	return &ScreenFrame{PNG: data, Image: img, CapturedAt: time.Now()}, nil
}

func (a *ADB) screenInput(ctx context.Context, serial string, args ...string) error {
	if a == nil || strings.TrimSpace(serial) == "" {
		return fmt.Errorf("请先配置 ADB 并选择设备")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := a.Run(ctx, serial, append([]string{"shell", "input"}, args...)...)
	if err != nil {
		return fmt.Errorf("设备 %s 输入失败：%w", serial, err)
	}
	return nil
}

func (a *ADB) ScreenTap(ctx context.Context, serial string, x, y int) error {
	if x < 0 || y < 0 {
		return fmt.Errorf("点击坐标无效")
	}
	return a.screenInput(ctx, serial, "tap", strconv.Itoa(x), strconv.Itoa(y))
}

func (a *ADB) ScreenSwipe(ctx context.Context, serial string, x1, y1, x2, y2, milliseconds int) error {
	if x1 < 0 || y1 < 0 || x2 < 0 || y2 < 0 || milliseconds < 1 || milliseconds > 2000 {
		return fmt.Errorf("滑动坐标或时长无效")
	}
	return a.screenInput(ctx, serial, "swipe", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2), strconv.Itoa(milliseconds))
}

func (a *ADB) ScreenKey(ctx context.Context, serial, key string) error {
	switch key {
	case "KEYCODE_BACK", "KEYCODE_HOME", "KEYCODE_APP_SWITCH", "KEYCODE_WAKEUP":
	default:
		return fmt.Errorf("不支持的设备按键 %q", key)
	}
	return a.screenInput(ctx, serial, "keyevent", key)
}
