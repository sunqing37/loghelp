package ui

import (
	"context"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/ncruces/zenity"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

func (w *app) toggleDeviceRecording() {
	d := w.preview
	if d == nil {
		return
	}
	if d.recordPreparing {
		w.cancelPreparingDeviceRecording(d)
		w.replaceDevicePreviewStream(d, core.ScreenRecordingOptions{})
		d.notice = "已取消录屏准备"
		w.updateDeviceChrome()
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
	options := w.config.ScreenRecording
	title := "保存设备录屏（MP4，无声音）"
	if options.SystemAudio {
		title = "保存设备录屏（MP4，系统声音）"
	}
	w.fileDialog("save", title, filepath.Join(w.dialogDirectory(""), name), zenity.FileFilters{{Name: "MP4 视频", Patterns: []string{"*.mp4"}, CaseFold: true}}, func(path string) {
		if w.preview != d || d.stream != stream || d.ctx.Err() != nil {
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".mp4") {
			w.message("保存录屏", "录屏请保存为 .mp4 文件。")
			return
		}
		w.prepareDeviceRecording(d, path, options)
	})
}

func (w *app) cancelPreparingDeviceRecording(d *devicePreview) {
	if d.recordPreparationCancel != nil {
		d.recordPreparationCancel()
		d.recordPreparationCancel = nil
	}
	d.recordPreparing = false
}

func (w *app) replaceDevicePreviewStream(d *devicePreview, options core.ScreenRecordingOptions) {
	w.stopDeviceClipboard(d)
	w.cancelDeviceTouch()
	previous := d.stream
	previous.Close()
	d.stream = core.StartScreenStreamWithOptions(d.ctx, d.client, d.serial, options, previous)
	w.previewStreams = append(w.previewStreams, d.stream)
	d.ready, d.failed = false, false
	d.statsAt, d.statsFrames = time.Now(), 0
	d.status = "正在连接视频投屏…"
	if d.texture != nil {
		d.texture.Dispose()
		d.texture = nil
	}
	d.frameSize = image.Point{}
}

func (w *app) prepareDeviceRecording(d *devicePreview, path string, options core.ScreenRecordingOptions) {
	if !options.SystemAudio && !options.ShowTouches {
		w.startPreparedDeviceRecording(d, d.stream, path, options)
		return
	}
	w.replaceDevicePreviewStream(d, options)
	stream := d.stream
	d.recordPreparing = true
	d.status = "正在准备录屏设置…"
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	d.recordPreparationCancel = cancel
	w.updateDeviceChrome()
	done := make(chan struct{})
	w.triggerWorkers = append(w.triggerWorkers, done)
	go func() {
		defer close(done)
		defer cancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var prepareErr error
		for {
			ready, _, err := stream.State()
			if err != nil || ready {
				prepareErr = err
				break
			}
			select {
			case <-ctx.Done():
				prepareErr = fmt.Errorf("录屏准备未完成：%w", ctx.Err())
			case <-ticker.C:
			}
			if prepareErr != nil {
				break
			}
		}
		w.post(func() {
			if w.preview != d || d.stream != stream || !d.recordPreparing {
				return
			}
			w.cancelPreparingDeviceRecording(d)
			if prepareErr != nil {
				// A failed stream waits for the device to come online again. A
				// preparation timeout still needs to cancel the running stream.
				if _, _, streamErr := stream.State(); streamErr == nil {
					w.replaceDevicePreviewStream(d, core.ScreenRecordingOptions{})
				}
				d.notice = prepareErr.Error()
				w.updateDeviceChrome()
				w.message("准备录屏失败", prepareErr.Error())
				return
			}
			w.startPreparedDeviceRecording(d, stream, path, options)
		})
	}()
}

func (w *app) startPreparedDeviceRecording(d *devicePreview, stream *core.ScreenStream, path string, options core.ScreenRecordingOptions) {
	recording, err := stream.StartRecording(path)
	if err != nil {
		if options.SystemAudio || options.ShowTouches {
			w.replaceDevicePreviewStream(d, core.ScreenRecordingOptions{})
		}
		w.updateDeviceChrome()
		w.message("开始录屏失败", err.Error())
		return
	}
	d.recording = recording
	d.recordStatus = ""
	d.notice = "录屏将保存至：" + path
	w.updateDeviceChrome()
	done := make(chan struct{})
	w.triggerWorkers = append(w.triggerWorkers, done)
	go func() {
		defer close(done)
		<-recording.Done()
		if options.SystemAudio || options.ShowTouches {
			stream.Close()
		}
		state := recording.State()
		message := ""
		if state.Path != "" {
			message = "录屏已保存：" + state.Path
		}
		if state.Err != nil {
			message = strings.TrimSpace(message + "\n" + state.Err.Error())
			support.Record("ERROR", "screen_recording.finished", message)
		} else {
			support.Record("INFO", "screen_recording.saved", message)
		}
		var revealErr error
		if state.Path != "" && options.RevealAfterSave {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			revealErr = core.RevealFile(ctx, state.Path)
			cancel()
			if revealErr != nil {
				message += "\n视频已保存，但定位文件失败：" + revealErr.Error()
				support.Record("ERROR", "screen_recording.reveal_failed", revealErr.Error())
			}
		}
		w.post(func() {
			if d.recording == recording {
				d.recording, d.recordStatus = nil, ""
			}
			if w.preview == d {
				_, _, streamErr := stream.State()
				if d.stream == stream && !d.failed && streamErr == nil && (options.SystemAudio || options.ShowTouches) {
					w.replaceDevicePreviewStream(d, core.ScreenRecordingOptions{})
				}
				d.notice = message
				w.updateDeviceChrome()
			}
			if state.Err != nil || revealErr != nil {
				w.message("录屏结束", message)
			} else if w.preview != d && !options.RevealAfterSave {
				w.message("录屏已保存", state.Path)
			}
		})
	}()
}

func (w *app) deviceRecordingSettings() {
	d := w.preview
	if d == nil || d.recording != nil || d.recordPreparing {
		return
	}
	w.showDeviceRecordingSettings()
}

func (w *app) showDeviceRecordingSettings() {
	options := w.config.ScreenRecording
	box := w.column(w.muted("下次录屏生效，设置保存在当前电脑"))
	add := func(title, description string, value *bool) {
		var button *widget.Button
		refresh := func() {
			if *value {
				button.SetText("已开启")
			} else {
				button.SetText("已关闭")
			}
		}
		button = w.action("", "quiet", func() { *value = !*value; refresh() })
		refresh()
		box.AddChild(w.stretchRow(w.label(title), button))
		text := w.muted(description)
		text.MaxWidth = float64(w.px(600))
		box.AddChild(text)
	}
	add("显示触控", "录制时显示设备触摸提示，结束后恢复设备原设置。", &options.ShowTouches)
	add("录制系统声音", "Android 11+；录入设备播放声音，不录麦克风。应用禁止采集时可能录到静音，Android 11 开始时需解锁。", &options.SystemAudio)
	add("保留设备播放", "内录时设备继续播放，需 Android 13+；关闭时采集系统输出，设备录制期间不外放。", &options.KeepDevicePlayback)
	add("结束后定位文件", "保存成功后自动打开文件管理器并定位视频；Linux 无选择服务时打开所在目录。", &options.RevealAfterSave)
	button := w.action("保存设置", "primary", func() {
		if !w.configWritable {
			w.showNestedDialog("保存设置失败", w.column(w.label("请先在偏好设置中处理配置文件异常，再保存录屏设置。")))
			return
		}
		candidate := w.config
		candidate.ScreenRecording = options
		if err := core.SaveConfig(candidate); err != nil {
			w.showNestedDialog("保存设置失败", w.column(w.label(err.Error())))
			return
		}
		w.config.ScreenRecording = options
		w.closeModal()
		if w.preview != nil {
			w.updateDeviceChrome()
		}
	})
	w.showDialog("录屏设置", box, button)
}

func (w *app) updateDeviceRecordingChrome(d *devicePreview) {
	d.record.SetText("开始录屏")
	d.record.GetWidget().Disabled = !d.ready || d.failed
	d.recordSettings.GetWidget().Disabled = d.recording != nil || d.recordPreparing
	if d.recordPreparing {
		d.record.SetText("取消准备")
		d.record.GetWidget().Disabled = false
	}
	if r := d.recording; r != nil {
		state := r.State()
		d.record.SetText("停止录屏")
		d.record.GetWidget().Disabled = state.Stopping
	}
	label := w.deviceRecordingLabel(d)
	d.recordLabel.Label = label
	d.recordStatus = label
}

func (w *app) deviceRecordingLabel(d *devicePreview) string {
	if d.recordPreparing {
		return "正在准备声音／触控…"
	}
	if d.recording == nil {
		label := "MP4 · 无声音"
		if w.config.ScreenRecording.SystemAudio {
			label = "MP4 · 系统声音"
		}
		if w.config.ScreenRecording.ShowTouches {
			label += " · 触控"
		}
		return label
	}
	state := d.recording.State()
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
	if d.recording == nil && !d.recordPreparing {
		return
	}
	if w.deviceRecordingLabel(d) != d.recordStatus {
		w.updateDeviceRecordingChrome(d)
		w.ui.Container.RequestRelayout()
		w.renderDirty = true
	}
}
