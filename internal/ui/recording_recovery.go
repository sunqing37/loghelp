package ui

import (
	"fmt"
	"path/filepath"

	"loghelp/internal/core"
)

func (w *app) recoverRecording(collector *core.Collector, hub *core.Hub) {
	status := collector.RecordingStatus()
	if status.Error == "" || status.Active {
		w.message("恢复录制", "该会话没有待恢复的录制故障，请刷新录制管理。")
		return
	}
	w.selectDirectory(filepath.Dir(status.Directory), func(path string) {
		options := core.OptionsFromConfig(w.config)
		options.Directory = path
		w.async(func() error {
			if err := hub.RecoverRecording(collector, options, status.Directory); err != nil {
				return fmt.Errorf("恢复录制失败，原故障记录保留，可重新选择目录：%w", err)
			}
			return nil
		}, func() {
			w.recordings()
		})
	})
}
