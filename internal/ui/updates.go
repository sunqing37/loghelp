package ui

import (
	"context"
	"strings"

	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
)

func (w *app) showUpdates() {
	content, check := w.updateSection()
	content.GetWidget().MinWidth = w.px(600)
	content.GetWidget().MinHeight = w.px(340)
	w.showNestedDialog("版本更新", content)
	check()
}

func (w *app) updateSection() (*widget.Container, func()) {
	var content *widget.Container
	status := w.label("手动检查 GitHub 上的最新正式版本")
	status.MaxWidth = float64(w.px(580))
	status.GetWidget().MinHeight = w.px(42)
	status.SetPosition(&widget.TextPositioning{HTextPosition: widget.TextPositionStart, VTextPosition: widget.TextPositionStart})
	notes := w.label("检查完成后将在这里显示发布说明。")
	notes.MaxWidth = float64(w.px(560))
	notes.SetPosition(&widget.TextPositioning{HTextPosition: widget.TextPositionStart, VTextPosition: widget.TextPositionStart})
	notesContent := w.column(notes)
	notesScroll := w.scrollBox(notesContent, w.px(170))
	relayout := func(owner *widget.Window) {
		notesContent.RequestRelayout()
		if content != nil {
			content.RequestRelayout()
		}
		owner.RequestRelayout()
		w.renderDirty = true
	}
	var latest core.ReleaseUpdate
	openLink := func(raw string) {
		owner := w.modalWindow
		go func() {
			if err := core.OpenReleaseURL(raw); err != nil {
				w.post(func() {
					if w.modalWindow == owner {
						status.Label = "打开链接失败：" + err.Error()
						relayout(owner)
					}
				})
			}
		}()
	}
	releaseButton := w.action("发布说明", "quiet", func() { openLink(latest.ReleaseURL) })
	updateButton := w.action("下载更新并重启", "primary", func() {
		message := w.label("更新将下载并校验新版，然后结束采集、录制和终端并重启应用。未保存的设置不会保留。是否继续？")
		message.MaxWidth = float64(w.px(580))
		w.showNestedDialog("更新至 "+latest.Label(), w.column(message, w.row(w.action("确认更新", "primary", func() {
			w.modalReturn = nil
			w.closeModal()
			w.installUpdate(latest)
		}), w.button("取消", w.closeModal))))
	})
	downloadButton := w.action("手动下载", "quiet", func() { openLink(latest.DownloadURL) })
	releaseButton.GetWidget().Disabled = true
	updateButton.GetWidget().Disabled = true
	downloadButton.GetWidget().Disabled = true
	var checkButton *widget.Button
	check := func() {
		owner := w.modalWindow
		w.updateRequests++
		checkButton.GetWidget().Disabled = true
		releaseButton.GetWidget().Disabled = true
		updateButton.GetWidget().Disabled = true
		downloadButton.GetWidget().Disabled = true
		status.Label = "正在检查更新…"
		notes.Label = "正在获取发布说明…"
		notesScroll.ScrollTop = 0
		relayout(owner)
		go func() {
			result, err := core.CheckRelease(w.ctx, appVersion, appRevision)
			w.post(func() {
				w.updateRequests--
				if w.modalWindow != owner {
					return
				}
				checkButton.GetWidget().Disabled = false
				if err != nil {
					status.Label = "检查失败：" + err.Error()
					notes.Label = "暂时无法获取发布说明，可点击“重新检查”重试。"
				} else {
					latest = result
					if result.Newer {
						status.Label = "发现更新 " + result.Label() + "（当前 " + appVersionLabel + "）"
					} else if result.Version == appVersion && result.Revision == appRevision {
						status.Label = "已是最新版本 " + appVersionLabel
					} else {
						status.Label = "当前 " + appVersionLabel + "；最新正式发布 " + result.Label() + "，无需更新"
					}
					releaseButton.GetWidget().Disabled = false
					updateButton.GetWidget().Disabled = !result.Newer || !core.AutomaticUpdateSupported() || !result.DownloadAvailable || result.Digest == ""
					downloadButton.GetWidget().Disabled = !result.Newer
					summary := []rune(strings.TrimSpace(result.Notes))
					if len(summary) > 2000 {
						summary = append(summary[:2000], []rune("\n…完整内容请打开发布说明。")...)
					}
					notes.Label = strings.TrimSpace(result.Name + "\n" + string(summary))
				}
				relayout(owner)
			})
		}()
	}
	checkButton = w.action("重新检查", "quiet", check)
	content = widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, false, true, false}), widget.GridLayoutOpts.Spacing(0, w.px(12)))))
	content.AddChild(w.stretchRow(w.label("LogHelp "+appVersionLabel), checkButton), status, notesScroll, w.row(releaseButton, updateButton, downloadButton))
	return content, check
}

func (w *app) installUpdate(release core.ReleaseUpdate) {
	if w.updateInProgress {
		w.message("更新中", "已有更新任务正在处理，请稍后重试。")
		return
	}
	w.updateInProgress = true
	ctx, cancel := context.WithCancel(w.ctx)
	status := w.label("正在下载并校验新版，完成后将自动替换程序并重启。")
	status.MaxWidth = float64(w.px(580))
	w.show("正在更新", w.column(status, w.button("取消更新", func() { cancel(); w.closeModal() })))
	owner := w.modalWindow
	go func() {
		prepared, err := core.PrepareUpdate(ctx, release)
		if w.ctx.Err() != nil {
			cancel()
			if prepared != nil {
				prepared.Cleanup()
			}
			return
		}
		w.post(func() {
			defer cancel()
			w.updateInProgress = false
			if ctx.Err() != nil || w.modalWindow != owner {
				if prepared != nil {
					prepared.Cleanup()
				}
				return
			}
			if err != nil {
				w.message("更新失败", err.Error()+"\n当前程序保持可用，可重新检查更新或手动下载。")
				return
			}
			if err := prepared.Start(); err != nil {
				prepared.Cleanup()
				w.message("更新失败", err.Error())
				return
			}
			status.Label = "校验通过，正在结束会话并重启…"
			w.cancel()
		})
	}()
}
