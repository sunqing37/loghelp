package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
)

func (w *app) showUpdates() {
	w.showUpdatesWithProxy(w.config.UpdateProxy)
}

func (w *app) updateProxySettings(value string) (*widget.Container, func() (string, error)) {
	mode, address := "环境变量", ""
	if value == "direct" {
		mode = "直连"
	} else if value != "" {
		mode, address = "自定义代理", value
	}
	entry := w.input(address, "http://127.0.0.1:7890", nil, nil)
	buttons := w.row()
	choices := map[string]*widget.Button{}
	refresh := func() {
		for title, button := range choices {
			if title == mode {
				button.SetText("● " + title)
			} else {
				button.SetText(title)
			}
		}
		entry.GetWidget().Disabled = mode != "自定义代理"
	}
	for _, title := range []string{"环境变量", "直连", "自定义代理"} {
		choices[title] = w.button(title, func() { mode = title; refresh() })
		buttons.AddChild(choices[title])
	}
	refresh()
	hint := w.muted("用于检查和下载更新；环境变量模式不读取系统代理。")
	hint.MaxWidth = float64(w.px(620))
	usage := w.muted("支持 HTTP、HTTPS、SOCKS5；检查可使用当前输入，保存后长期生效。")
	usage.MaxWidth = float64(w.px(620))
	box := w.column(w.heading("更新网络"), buttons, w.inputRow("代理地址", entry), hint, usage)
	return box, func() (string, error) {
		switch mode {
		case "环境变量":
			return "", nil
		case "直连":
			return "direct", nil
		default:
			address := strings.TrimSpace(entry.GetText())
			if address == "" || address == "direct" {
				return "", fmt.Errorf("请填写自定义代理地址，例如 http://127.0.0.1:7890")
			}
			return address, core.ValidateUpdateProxy(address)
		}
	}
}

func (w *app) showUpdatesWithProxy(proxy string) {
	content, check := w.updateSection(proxy)
	content.GetWidget().MinWidth = w.px(600)
	content.GetWidget().MinHeight = w.px(340)
	w.showNestedDialog("版本更新", content)
	check()
}

func (w *app) updateSection(proxy string) (*widget.Container, func()) {
	var content *widget.Container
	status := w.label("手动检查 GitHub 上的最新正式版本")
	status.MaxWidth = float64(w.px(580))
	status.GetWidget().MinHeight = w.px(42)
	status.SetPosition(&widget.TextPositioning{HTextPosition: widget.TextPositionStart, VTextPosition: widget.TextPositionStart})
	var notes *markdownView
	var notesScroll *widget.ScrollContainer
	relayout := func(owner *widget.Window) {
		notes.RequestRelayout()
		if content != nil {
			content.RequestRelayout()
		}
		owner.RequestRelayout()
		w.renderDirty = true
	}
	latest := core.ReleaseUpdate{ReleaseURL: core.LatestReleaseURL, DownloadURL: core.LatestReleaseURL}
	openURL := func(raw string, opener func(string) error) {
		owner := w.modalWindow
		go func() {
			if err := opener(raw); err != nil {
				w.post(func() {
					if w.modalWindow == owner {
						status.Label = "打开链接失败：" + err.Error()
						relayout(owner)
					}
				})
			}
		}()
	}
	openLink := func(raw string) { openURL(raw, core.OpenReleaseURL) }
	notes = w.markdown(func(raw string) { openURL(raw, core.OpenReleaseNotesURL) })
	notes.setPlain("检查完成后将在这里显示发布说明。")
	notesScroll = w.scrollBox(notes, w.px(170))
	notes.viewport = notesScroll.ViewRect
	notes.scrollTo = func(y float64) {
		_, height := notes.PreferredSize()
		notesScroll.ScrollTop = markdownScrollPosition(y, height, notesScroll.ViewRect().Dy())
		w.renderDirty = true
	}
	releaseButton := w.action("发布说明", "quiet", func() { openLink(latest.ReleaseURL) })
	updateButton := w.action("下载更新并重启", "primary", func() {
		message := w.label("更新将下载并校验新版，然后结束采集、录制和终端并重启应用。未保存的设置不会保留。是否继续？")
		message.MaxWidth = float64(w.px(580))
		w.showNestedDialog("更新至 "+latest.Label(), w.column(message, w.row(w.action("确认更新", "primary", func() {
			w.modalReturn = nil
			w.closeModal()
			w.installUpdate(latest, proxy)
		}), w.button("取消", w.closeModal))))
	})
	downloadButton := w.action("打开发布页手动下载", "quiet", func() { openLink(latest.ReleaseURL) })
	releaseButton.GetWidget().Disabled = true
	updateButton.GetWidget().Disabled = true
	var checkButton *widget.Button
	check := func() {
		owner := w.modalWindow
		w.cancelUpdateCheck()
		ctx, cancel := context.WithCancel(w.ctx)
		w.updateCheckCancel = cancel
		latest = core.ReleaseUpdate{ReleaseURL: core.LatestReleaseURL, DownloadURL: core.LatestReleaseURL}
		w.updateRequests++
		checkButton.GetWidget().Disabled = true
		releaseButton.GetWidget().Disabled = true
		updateButton.GetWidget().Disabled = true
		status.Label = "正在检查更新…"
		notes.setPlain("正在获取发布说明…")
		notesScroll.ScrollTop = 0
		notesScroll.ScrollLeft = 0
		relayout(owner)
		go func() {
			defer cancel()
			result, err := core.CheckRelease(ctx, appVersion, appRevision, core.UpdateNetworkOptions{Proxy: proxy, Attempt: func(attempt, total int) {
				w.post(func() {
					if w.modalWindow == owner && ctx.Err() == nil {
						status.Label = fmt.Sprintf("正在检查更新（第 %d/%d 次）…", attempt, total)
						relayout(owner)
					}
				})
			}})
			w.post(func() {
				w.updateRequests--
				if w.modalWindow != owner {
					return
				}
				w.updateCheckCancel = nil
				checkButton.GetWidget().Disabled = false
				if err != nil {
					status.Label = "检查失败，可重新检查或打开发布页手动下载"
					notes.setPlain(err.Error() + "\n\n可关闭弹窗调整更新代理，或点击下方按钮用浏览器打开发布页。")
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
					body := strings.TrimSpace(result.Notes)
					if body == "" {
						body = strings.TrimSpace(result.Name)
					}
					notes.setMarkdown(body, result.ReleaseURL)
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

func (w *app) cancelUpdateCheck() {
	if w.updateCheckCancel != nil {
		w.updateCheckCancel()
		w.updateCheckCancel = nil
	}
}

func (w *app) installUpdate(release core.ReleaseUpdate, proxy string) {
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
		prepared, err := core.PrepareUpdate(ctx, release, core.UpdateNetworkOptions{Proxy: proxy})
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
