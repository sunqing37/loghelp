package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncruces/zenity"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

func (w *app) toggleDeviceRecording() {
	d := w.preview
	if d == nil {
		return
	}
	if d.recording != nil {
		d.recording.Stop()
		w.updateDeviceChrome()
		return
	}
	if !d.ready || d.failed {
		return
	}
	w.cancelDeviceTouch()
	name := "screenrecord-" + time.Now().Format("20060102-150405.000") + ".mp4"
	stream := d.stream
	w.fileDialog("save", "保存设备录屏（MP4，无声音）", filepath.Join(w.dialogDirectory(""), name), zenity.FileFilters{{Name: "MP4 视频", Patterns: []string{"*.mp4"}, CaseFold: true}}, func(path string) {
		if w.preview != d || d.stream != stream || d.ctx.Err() != nil {
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".mp4") {
			w.message("保存录屏", "录屏请保存为 .mp4 文件。")
			return
		}
		recording, err := stream.StartRecording(path)
		if err != nil {
			w.message("开始录屏失败", err.Error())
			return
		}
		d.recording = recording
		d.recordStatus = ""
		d.notice = "录屏将保存至：" + path
		w.updateDeviceChrome()
		go func() {
			<-recording.Done()
			state := recording.State()
			message := ""
			if state.Path != "" {
				message = "录屏已保存：" + state.Path
			}
			if state.Err != nil {
				if message != "" {
					message += "\n"
				}
				message += state.Err.Error()
				support.Record("ERROR", "screen_recording.finished", message)
			} else {
				support.Record("INFO", "screen_recording.saved", message)
			}
			w.post(func() {
				if d.recording == recording {
					d.recording = nil
					d.recordStatus = ""
				}
				if w.preview == d {
					d.notice = message
					w.updateDeviceChrome()
				}
				// Also report results after the original preview was closed/switched.
				if state.Err != nil {
					w.message("录屏结束", message)
				} else if w.preview != d {
					w.message("录屏已保存", state.Path)
				}
			})
		}()
	})
}

func (w *app) updateDeviceRecordingChrome(d *devicePreview) {
	d.record.SetText("开始录屏")
	d.record.GetWidget().Disabled = !d.ready || d.failed
	if r := d.recording; r != nil {
		state := r.State()
		d.record.SetText("停止录屏")
		d.record.GetWidget().Disabled = state.Stopping
	}
	label := deviceRecordingLabel(d.recording)
	d.recordLabel.Label = label
	d.recordStatus = label
}

func deviceRecordingLabel(recording *core.ScreenRecording) string {
	if recording == nil {
		return "MP4 · 无声音"
	}
	state := recording.State()
	switch {
	case state.Stopping:
		return "正在保存…"
	case state.StartedAt.IsZero():
		return "等待关键帧…"
	default:
		seconds := int(time.Since(state.StartedAt).Seconds())
		if state.Switching {
			return fmt.Sprintf("切屏中 %02d:%02d", seconds/60, seconds%60)
		}
		return fmt.Sprintf("录制中 %02d:%02d", seconds/60, seconds%60)
	}
}

func (w *app) pollDeviceRecording(d *devicePreview) {
	if d.recording == nil {
		return
	}
	if deviceRecordingLabel(d.recording) != d.recordStatus {
		w.updateDeviceRecordingChrome(d)
		w.ui.Container.RequestRelayout()
		w.renderDirty = true
	}
}
