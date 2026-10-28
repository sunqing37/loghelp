package core

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var activityComponent = regexp.MustCompile(`(?:^|\s)([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)/[A-Za-z0-9_.$]+`)

// ForegroundPackage reads activity state rather than window focus, which can
// belong to an input method or a transient system window.
func (a *ADB) ForegroundPackage(ctx context.Context, serial string) (string, error) {
	if strings.TrimSpace(serial) == "" {
		return "", fmt.Errorf("读取顶部应用：请先选择设备")
	}
	out, err := a.Run(ctx, serial, "shell", "dumpsys", "activity", "activities")
	if err != nil {
		return "", fmt.Errorf("读取设备 %s 顶部应用：%w", serial, err)
	}
	// Prefer the single top-resumed activity on Android 10+. Older systems
	// report a resumed activity per stack; reject conflicting packages.
	lines := strings.Split(out, "\n")
	for _, markers := range [][]string{
		{"topResumedActivity=", "mTopResumedActivity="},
		{"mResumedActivity:", "mResumedActivity=", "ResumedActivity:"},
	} {
		found, pkg := false, ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			for _, marker := range markers {
				if !strings.HasPrefix(line, marker) {
					continue
				}
				found = true
				match := activityComponent.FindStringSubmatch(strings.TrimPrefix(line, marker))
				if len(match) == 0 {
					continue
				}
				if pkg != "" && pkg != match[1] {
					return "", fmt.Errorf("设备 %s 有多个活动应用，暂无法确定顶部包名", serial)
				}
				pkg = match[1]
			}
		}
		if pkg != "" {
			return strings.Clone(pkg), nil
		}
		if found {
			break
		}
	}
	return "", fmt.Errorf("设备 %s 暂无可识别的顶部应用，保留当前包名", serial)
}
