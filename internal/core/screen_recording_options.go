package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ScreenRecordingOptions are local preferences; device settings are scoped to
// the recording's scrcpy session and restored by its cleanup service.
type ScreenRecordingOptions struct {
	ShowTouches        bool `json:"show_touches"`
	SystemAudio        bool `json:"system_audio"`
	KeepDevicePlayback bool `json:"keep_device_playback"`
	RevealAfterSave    bool `json:"reveal_after_save"`
}

func waitScreenTouches(parent context.Context, adb *ADB, serial string) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	for {
		value, err := adb.Run(ctx, serial, "shell", "settings", "get", "system", "show_touches")
		if err != nil {
			return fmt.Errorf("检查触控显示失败：%w", err)
		}
		if strings.TrimSpace(value) == "1" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("设备未能开启触控显示，请检查设备权限或关闭「显示触控」后重试")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
