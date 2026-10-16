package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ncruces/zenity"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

func (w *app) diagnosticText(value string) *widget.Text {
	width := min(w.px(620), w.width-w.px(128))
	label := w.label(w.wirelessText(value, width))
	label.MaxWidth = float64(width)
	return label
}

func (w *app) showDiagnosticNotice() {
	count, warning := w.support.PendingCount(), w.support.Warning()
	if w.diagnosticErr != nil {
		warning = w.diagnosticErr.Error()
	}
	if count == 0 && warning == "" {
		return
	}
	w.diagnosticWarning = warning
	box := w.column()
	if count > 0 {
		box.AddChild(w.diagnosticText(fmt.Sprintf("发现 %d 次未记录正常退出的运行。可能是程序异常、强制结束或断电；如有 Go 崩溃堆栈，已保存在本机。", count)))
	}
	if warning != "" {
		box.AddChild(w.diagnosticText("本地诊断未完整启用：" + warning + "\n请检查目录权限或磁盘空间。"))
	}
	box.AddChild(w.diagnosticText("日志不会自动上传。可在问题反馈中查看本地目录、预览并导出脱敏诊断包。"), w.button("问题反馈与诊断", w.showFeedback))
	if count > 0 {
		box.AddChild(w.button("已知晓，保留记录", func() {
			owner := w.modalWindow
			w.async(w.support.Acknowledge, func() {
				if w.modalWindow == owner {
					w.closeModal()
				}
			})
		}))
	}
	w.showNestedDialog("运行诊断提示", box)
}

func (w *app) showFeedback() {
	summary := w.input("", "发生了什么问题", nil, nil)
	steps := w.input("", "按顺序填写操作步骤", nil, nil)
	expected := w.input("", "希望出现的结果", nil, nil)
	actual := w.input("", "实际出现的结果", nil, nil)
	box := w.column(w.diagnosticText("只在本机生成 ZIP，不自动上传。请勿填写设备日志、账号、配对码或其他敏感内容；每项最多 4096 字节。"))
	if w.support != nil {
		box.AddChild(w.diagnosticText("本地日志目录："+w.support.Root), w.diagnosticText("运行日志每份 1 MiB，最多 3 份/会话；启动时清理超过 14 天或最近 20 次以外的会话。原始崩溃记录仅供本地排查，请勿直接分享。"))
		box.AddChild(w.button("打开本地日志目录", func() { w.async(func() error { return core.OpenLocalDirectory(w.support.Root) }, nil) }))
	} else {
		box.AddChild(w.diagnosticText("本次运行未启用日志记录，仍可导出环境信息和问题描述。"))
	}
	box.AddChild(w.inputRow("问题概述", summary), w.inputRow("复现步骤", steps), w.inputRow("预期结果", expected), w.inputRow("实际结果", actual))
	var screenshots []support.Screenshot
	var preparing bool
	attachments := w.column()
	var feedbackWindow *widget.Window
	var refreshAttachments func()
	refreshAttachments = func() {
		attachments.RemoveChildren()
		for index, shot := range screenshots {
			attachments.AddChild(w.row(w.button(fmt.Sprintf("查看截图 %d · %d × %d", index+1, shot.Width, shot.Height), func() {
				if !preparing {
					w.previewScreenshot("截图预览", shot)
				}
			}), w.button("移除", func() {
				if preparing {
					return
				}
				copy(screenshots[index:], screenshots[index+1:])
				screenshots[len(screenshots)-1] = support.Screenshot{}
				screenshots = screenshots[:len(screenshots)-1]
				refreshAttachments()
			})))
		}
		box.RequestRelayout()
		if feedbackWindow != nil {
			feedbackWindow.RequestRelayout()
		}
		w.renderDirty = true
	}
	var prepare *widget.Button
	var addScreenshot *widget.Button
	addScreenshot = w.button("添加截图", func() {
		if preparing {
			return
		}
		if len(screenshots) >= support.MaxScreenshots {
			w.showNestedDialog("截图数量已达上限", w.column(w.diagnosticText("最多添加 3 张截图，可先移除不需要的图片。")))
			return
		}
		w.fileDialog("open", "选择反馈截图", w.dialogDirectory(""), zenity.FileFilters{{Name: "PNG / JPEG 截图", Patterns: []string{"*.png", "*.jpg", "*.jpeg"}, CaseFold: true}}, func(path string) {
			if w.modalWindow != feedbackWindow {
				return
			}
			addScreenshot.GetWidget().Disabled, prepare.GetWidget().Disabled = true, true
			go func() {
				shot, err := support.LoadScreenshot(w.ctx, path)
				w.post(func() {
					addScreenshot.GetWidget().Disabled, prepare.GetWidget().Disabled = false, false
					if w.modalWindow != feedbackWindow {
						return
					}
					if err != nil {
						w.showNestedDialog("添加截图失败", w.column(w.diagnosticText(err.Error())))
						return
					}
					screenshots = append(screenshots, shot)
					refreshAttachments()
				})
			}()
		})
	})
	box.AddChild(w.heading("截图附件"), w.diagnosticText("最多 3 张 PNG/JPEG，每张不超过 8 MiB。截图随诊断包保存，不自动上传；图片中的文字不会自动打码，请先遮挡隐私内容。"), attachments, addScreenshot)
	prepare = w.button("预览并导出脱敏诊断包", func() {
		if preparing {
			return
		}
		preparing = true
		addScreenshot.GetWidget().Disabled = true
		feedback := support.Feedback{Summary: summary.GetText(), Steps: steps.GetText(), Expected: expected.GetText(), Actual: actual.GetText(), Screenshots: append([]support.Screenshot(nil), screenshots...)}
		owner := w.modalWindow
		prepare.GetWidget().Disabled = true
		go func() {
			report, err := w.support.BuildReport(w.ctx, feedback)
			w.post(func() {
				preparing = false
				addScreenshot.GetWidget().Disabled = false
				prepare.GetWidget().Disabled = false
				if w.modalWindow != owner {
					return
				}
				if err != nil {
					support.Record("ERROR", "report.prepare_failed", err.Error())
					w.showNestedDialog("诊断包准备失败", w.column(w.diagnosticText(err.Error())))
					return
				}
				w.previewReport(report)
			})
		}()
	})
	box.AddChild(prepare, w.diagnosticText("可逐项查看将要导出的内容。自动诊断不含原始错误文本、路径和设备数据；手填描述仍需人工检查。"), w.button("打开 GitHub 问题页", func() {
		w.async(func() error { return core.OpenReleaseNotesURL("https://github.com/sunqing37/loghelp/issues/new") }, nil)
	}), w.diagnosticText("打开网页不会附带描述或诊断包，请检查内容后自行填写和提交。"))
	w.showNestedDialog("问题反馈与诊断", box)
	feedbackWindow = w.modalWindow
}

func (w *app) previewReport(report *support.Report) {
	box := w.column(w.diagnosticText("以下文件是本次导出的固定快照。点击文件查看全部内容，确认后选择新的 ZIP 文件名；不会覆盖已有文件。"))
	box.AddChild(w.button("确认内容并保存 ZIP", func() {
		name := "loghelp-diagnostics-" + time.Now().Format("20060102-150405") + ".zip"
		w.fileDialog("save", "保存脱敏诊断包（使用新的文件名）", filepath.Join(w.dialogDirectory(""), name), zenity.FileFilters{{Name: "ZIP 诊断包", Patterns: []string{"*.zip"}, CaseFold: true}}, func(path string) {
			w.async(func() error { return report.Save(w.ctx, path) }, func() {
				support.Record("INFO", "report.exported", "")
				w.showNestedDialog("诊断包已保存", w.column(w.diagnosticText("已保存至："+path+"\n没有上传。请检查包内内容后，再自行提交反馈。")))
			})
		})
	}))
	for _, file := range report.Files {
		box.AddChild(w.button(fmt.Sprintf("查看 %s · %d 字节", file.Name, len(file.Data)), func() {
			if shot, ok := report.Screenshots[file.Name]; ok {
				w.previewScreenshot(file.Name, shot)
			} else {
				w.previewReportFile(file)
			}
		}))
	}
	w.showNestedDialog("诊断包预览", box)
}

func (w *app) previewScreenshot(title string, shot support.Screenshot) {
	source := ebiten.NewImageFromImage(shot.Thumbnail)
	texture := ebiten.NewImage(w.px(shot.Thumbnail.Bounds().Dx()), w.px(shot.Thumbnail.Bounds().Dy()))
	options := &ebiten.DrawImageOptions{}
	options.GeoM.Scale(w.displayScale, w.displayScale)
	texture.DrawImage(source, options)
	source.Deallocate()
	w.showNestedDialog(title, w.column(w.diagnosticText(fmt.Sprintf("原图 %d × %d · 预览缩放显示；图片内容未自动打码。", shot.Width, shot.Height)), widget.NewGraphic(widget.GraphicOpts.Image(texture))))
	back := w.modalReturn
	w.modalReturn = func() {
		texture.Deallocate()
		if back != nil {
			back()
		}
	}
}

// Bound each text layout while still making every exported byte inspectable.
func (w *app) previewReportFile(file support.ReportFile) {
	runes := []rune(string(file.Data))
	const pageSize = 4000
	page, pages := 0, max(1, (len(runes)+pageSize-1)/pageSize)
	view := w.markdown(func(string) {})
	scroll := w.scrollBox(view, w.px(300))
	view.viewport = scroll.ViewRect
	position := w.label("")
	var previous, next *widget.Button
	var box *widget.Container
	var owner *widget.Window
	refresh := func() {
		start := page * pageSize
		view.setPlain(w.wirelessText(string(runes[start:min(len(runes), start+pageSize)]), view.preferredWidth()))
		scroll.ScrollTop = 0
		position.Label = fmt.Sprintf("第 %d / %d 页", page+1, pages)
		previous.GetWidget().Disabled, next.GetWidget().Disabled = page == 0, page+1 == pages
		if owner != nil {
			box.RequestRelayout()
			owner.RequestRelayout()
			w.renderDirty = true
		}
	}
	previous = w.button("上一页", func() {
		if page > 0 {
			page--
			refresh()
		}
	})
	next = w.button("下一页", func() {
		if page+1 < pages {
			page++
			refresh()
		}
	})
	refresh()
	box = w.column(w.row(previous, position, next), scroll)
	box.GetWidget().MinWidth, box.GetWidget().MinHeight = w.px(600), w.px(360)
	w.showNestedDialog(file.Name, box)
	owner = w.modalWindow
}
