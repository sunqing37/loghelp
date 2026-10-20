package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func RevealFile(ctx context.Context, path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("定位录屏文件：%w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("定位录屏文件 %s：%w", absolute, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("无法定位录屏：%s 不是普通文件", absolute)
	}
	if err := revealFileNative(ctx, absolute); err != nil {
		return fmt.Errorf("打开录屏文件位置 %s：%w", absolute, err)
	}
	return nil
}
