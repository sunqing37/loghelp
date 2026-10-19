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
	"golang.org/x/image/draw"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

func (w *app) issueBundle() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	from, to := w.input("", "留空不限，YYYY-MM-DD HH:MM:SS 或 RFC3339", nil, nil), w.input("", "包含终点", nil, nil)
	note := w.input("", "复现步骤和备注（最多 4096 字节）", nil, nil)
	redact, device, shot := true, p.offline == nil && p.collector != nil, false
	box := w.column(w.diagnosticText("导出当前过滤后的完整来源，实时使用当前缓存/暂停快照。按设备日志时间选择范围，起止均包含；有时间条件时排除无设备时间和采集事件。预览后保存固定 ZIP，原始日志不变。"), w.inputRow("开始时间", from), w.inputRow("结束时间", to), w.inputRow("备注", note))
	toggle := func(label string, value *bool) {
		var b *widget.Button
		text := func() string {
			if *value {
				return label + "：开启"
			}
			return label + "：关闭"
		}
		b = w.button(text(), func() { *value = !*value; b.SetText(text()) })
		box.AddChild(b)
	}
	toggle("常见敏感文本脱敏", &redact)
	toggle("读取当前设备和应用版本", &device)
	toggle("拍摄当前设备截图", &shot)
	box.AddChild(w.diagnosticText("文本脱敏不能保证覆盖所有敏感内容；截图不会自动打码。截图记录实际拍摄时间，不能代替问题发生时的画面。离线来源也只读取当前所填设备的信息。"), w.button("生成预览", func() {
		if p.dead || w.active() != p {
			return
		}
		w.syncInputs()
		opt := core.EvidenceOptions{Note: note.GetText(), Serial: p.state.Serial, Package: p.state.Package, Redact: redact}
		var err error
		if strings.TrimSpace(from.GetText()) != "" {
			opt.From, err = core.ParseTime(from.GetText())
			if err != nil {
				w.message("开始时间无效", err.Error())
				return
			}
		}
		if strings.TrimSpace(to.GetText()) != "" {
			opt.To, err = core.ParseTime(to.GetText())
			if err != nil {
				w.message("结束时间无效", err.Error())
				return
			}
		}
		if !opt.From.IsZero() && !opt.To.IsZero() && opt.From.After(opt.To) {
			w.message("时间范围无效", "结束时间不能早于开始时间。")
			return
		}
		if len(opt.Note) > 4096 {
			w.message("备注过长", "备注最多 4096 字节。")
			return
		}
		client := w.client
		if (device || shot) && (client == nil || opt.Serial == "") {
			w.message("设备未选择", "请先选择设备，或关闭设备信息/截图选项。")
			return
		}
		s := w.investigationSource(p)
		opt.Scope = s.scope
		ctx, job, _ := w.beginPageJob(p, "准备 Android 问题包", 0)
		finish := w.showAnalysisProgress(p, ctx, job, "准备 Android 问题包")
		go func() {
			lookup, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			var workErr error
			if device {
				opt.Info, workErr = client.EvidenceInfo(lookup, opt.Serial, opt.Package)
			}
			if workErr == nil && shot {
				opt.Screenshot, workErr = client.CaptureScreen(lookup, opt.Serial)
			}
			var report *support.Report
			if workErr == nil {
				report, workErr = core.BuildEvidence(ctx, s.scan, opt)
			}
			if workErr == nil && opt.Screenshot != nil {
				frame := opt.Screenshot
				bounds := frame.Image.Bounds()
				width := min(480, bounds.Dx())
				height := max(1, bounds.Dy()*width/bounds.Dx())
				if height > 360 {
					height = 360
					width = max(1, bounds.Dx()*height/bounds.Dy())
				}
				thumb := image.NewRGBA(image.Rect(0, 0, width, height))
				draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), frame.Image, bounds, draw.Src, nil)
				report.Screenshots = map[string]support.Screenshot{"screenshot.png": {PNG: frame.PNG, Thumbnail: thumb, Width: bounds.Dx(), Height: bounds.Dy()}}
			}
			w.post(func() {
				if !finish() {
					return
				}
				if workErr != nil {
					w.message("问题包准备失败", workErr.Error())
					return
				}
				w.previewIssueBundle(report)
			})
		}()
	}))
	w.show("导出 Android 问题包", box)
}

func (w *app) previewIssueBundle(report *support.Report) {
	box := w.column(w.diagnosticText("文件已固定为这次预览的快照。逐项检查日志、元数据和图片后再保存；本工具不会自动上传。"))
	for _, file := range report.Files {
		file := file
		box.AddChild(w.button(fmt.Sprintf("查看 %s · %d 字节", file.Name, len(file.Data)), func() {
			if shot, ok := report.Screenshots[file.Name]; ok {
				w.previewScreenshot(file.Name, shot)
			} else {
				w.previewReportFile(file)
			}
		}))
	}
	box.AddChild(w.button("保存问题包 ZIP", func() {
		w.fileDialog("save", "保存 Android 问题包（新文件名）", filepath.Join(w.dialogDirectory(""), core.EvidenceFilename()), zenity.FileFilters{{Name: "ZIP", Patterns: []string{"*.zip"}}}, func(path string) {
			w.show("保存问题包", w.column(w.label("正在写入，可关闭取消…")))
			owner := w.modalWindow
			ctx, cancel := context.WithCancel(w.ctx)
			w.analysisCancel = cancel
			go func() {
				err := report.Save(ctx, path)
				w.post(func() {
					defer cancel()
					if w.modalWindow != owner {
						return
					}
					w.analysisCancel = nil
					if err != nil {
						w.message("保存失败", err.Error())
					} else {
						w.message("问题包已保存", path)
					}
				})
			}()
		})
	}))
	w.show("Android 问题包预览", box)
}
