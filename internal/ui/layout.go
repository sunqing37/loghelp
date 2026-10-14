package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui"
	uiimage "github.com/ebitenui/ebitenui/image"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"loghelp/internal/core"
)

const controlHeight = 36

func (w *app) label(s string) *widget.Text { return w.textLabel(s, &w.uiFace, w.foreground()) }
func (w *app) statusText(s string) *widget.Text {
	return widget.NewText(widget.TextOpts.WidgetOpts(widget.WidgetOpts.MinSize(0, 26)), widget.TextOpts.Text(s, &w.smallFace, w.colors().muted), widget.TextOpts.Position(widget.TextPositionStart, widget.TextPositionCenter), widget.TextOpts.MaxWidth(float64(w.width-40)))
}
func (w *app) button(s string, fn func()) *widget.Button { return w.action(s, "", fn) }
func (w *app) buttonColor() color.NRGBA                  { return w.colors().elevated }
func (w *app) input(value, placeholder string, change func(string), submit func()) *boundedInput {
	value = singleLineInput(value)
	c := w.colors()
	_, fontHeight := text.Measure(" ", w.uiFace, 0)
	verticalPadding := max(6, (controlHeight-int(math.Ceil(fontHeight)))/2)
	images := &widget.TextInputImage{Idle: w.skin(c.bg, c.line, 6), Disabled: w.skin(c.surface, c.line, 6)}
	entry := widget.NewTextInput(widget.TextInputOpts.WidgetOpts(widget.WidgetOpts.MinSize(100, controlHeight), w.hoverCursor(ebiten.CursorShapeText)), widget.TextInputOpts.Image(images), widget.TextInputOpts.Face(&w.uiFace), widget.TextInputOpts.Color(&widget.TextInputColor{Idle: c.text, Caret: c.accent, Disabled: c.muted, DisabledCaret: c.muted}), widget.TextInputOpts.Padding(&widget.Insets{Left: 12, Right: 12, Top: verticalPadding, Bottom: verticalPadding}), widget.TextInputOpts.Placeholder(placeholder), widget.TextInputOpts.ChangedHandler(func(a *widget.TextInputChangedEventArgs) {
		if change != nil {
			change(a.InputText)
		}
	}), widget.TextInputOpts.SubmitHandler(func(*widget.TextInputChangedEventArgs) {
		if submit != nil {
			submit()
		}
	}))
	entry.GetWidget().FocusEvent.AddHandler(func(a any) {
		if event, ok := a.(*widget.WidgetFocusEventArgs); ok {
			border := c.line
			if event.Focused {
				border = c.accent
			}
			images.Idle = w.skin(c.bg, border, 6)
		}
	})
	entry.SetText(value)
	input := &boundedInput{TextInput: entry, editText: value, cursor: len([]rune(value)), anchor: -1,
		face: &w.uiFace, images: images, padding: widget.Insets{Left: 12, Right: 12, Top: verticalPadding, Bottom: verticalPadding},
		foreground: c.text, muted: c.muted, placeholder: placeholder,
		legacyDiagnostic: os.Getenv("LOGHELP_PROFILE_DIR") != "" && os.Getenv("LOGHELP_PROFILE_LEGACY_INPUTS") == "1"}
	entry.GetWidget().CustomData = input
	return input
}
func row(children ...widget.PreferredSizeLocateableWidget) *widget.Container {
	box := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewRowLayout(widget.RowLayoutOpts.Direction(widget.DirectionHorizontal), widget.RowLayoutOpts.Spacing(5))))
	for _, child := range children {
		data, _ := child.GetWidget().LayoutData.(widget.RowLayoutData)
		data.Position = widget.RowLayoutPositionCenter
		child.GetWidget().LayoutData = data
	}
	box.AddChild(children...)
	return box
}
func column(children ...widget.PreferredSizeLocateableWidget) *widget.Container {
	box := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, nil), widget.GridLayoutOpts.Spacing(0, 6))))
	box.AddChild(children...)
	return box
}
func (w *app) inputRow(label string, entry *boundedInput, buttons ...widget.PreferredSizeLocateableWidget) *widget.Container {
	box := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(3), widget.GridLayoutOpts.Stretch([]bool{false, true, false}, nil), widget.GridLayoutOpts.Spacing(8, 0))))
	caption := w.label(label)
	if label != "过滤" && label != "查找" && label != "命令" {
		caption.GetWidget().MinWidth = 138
	}
	box.AddChild(caption, entry, row(buttons...))
	return box
}

func (w *app) rebuild() {
	w.finishColumnResize()
	w.cursorShape = ebiten.CursorShapeDefault
	w.tabWidgets = nil
	w.tabDrag = tabDragState{}
	w.detailArea, w.detailSplitter = nil, nil
	w.detailDragging, w.detailDragChanged = false, false
	for _, input := range []*boundedInput{w.serialInput, w.appInput, w.buffersInput, w.filterInput, w.searchInput, w.commandInput} {
		input.disposeBuffer()
	}
	w.serialInput, w.appInput, w.buffersInput, w.filterInput, w.searchInput, w.commandInput = nil, nil, nil, nil, nil, nil
	w.sourceLabel, w.resultLabel, w.statusLabel = nil, nil, nil
	w.renderDirty = true
	defer func() {
		if w.modal && w.modalWindow != nil {
			w.removeModal = w.ui.AddWindow(w.modalWindow)
		}
	}()
	p := w.active()
	w.startButton, w.followButton, w.recordButton, w.pauseButton = nil, nil, nil, nil
	w.copyDetailButton = nil
	w.connectionLabel = nil
	c := w.colors()
	root := widget.NewContainer(widget.ContainerOpts.BackgroundImage(uiimage.NewNineSliceColor(c.bg)), widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, true, false}))))
	brand := row(w.action("L", "active", func() { w.help() }), w.heading("LogHelp"), w.muted(appVersionLabel), w.muted("ANDROID LOG WORKSPACE"))
	chrome := stretchRow(brand, row(w.action("打开文件", "quiet", w.openFile), w.action("终端", "quiet", w.openTerminal), w.action("录制管理", "quiet", w.recordings), w.action("设置", "quiet", w.settings)))
	root.AddChild(w.inset(chrome, 12, c.surface))
	w.ui = &ebitenui.UI{Container: root, PostRenderHook: w.drawPanels}
	if p == nil {
		return
	}
	workspace := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(2), widget.GridLayoutOpts.Stretch([]bool{false, true}, []bool{true}))))
	workspace.AddChild(w.sidebar(p))
	main := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, false, false, false, true, false}), widget.GridLayoutOpts.Spacing(0, 8))))
	main.AddChild(w.tabStrip())
	if p.shell != nil {
		w.serialInput = nil
		command := w.input("", "输入命令，按 Enter 提交", nil, nil)
		w.commandInput = command
		command.SubmitEvent.AddHandler(func(any) { w.submitShell(p, command.GetText()); command.SetText("") })
		main.AddChild(w.inset(w.inputRow("命令", command, w.action("提交", "primary", func() { w.submitShell(p, command.GetText()); command.SetText("") }), w.action("历史", "quiet", func() { w.choices("命令历史", w.config.ShellHistory, func(s string) { command.SetText(s) }) })), 12, c.surface))
		main.AddChild(stretchRow(w.label("设备终端"), row(w.action("重新连接", "quiet", func() { w.connectShell(p) }), w.action("复制输出", "quiet", func() { w.copyTerminal(p) }))))
		main.AddChild(w.muted("点击终端输入  ·  Ctrl+C 中断  ·  Ctrl+Shift+C/V 复制粘贴"))
		w.logPanel = widget.NewContainer(widget.ContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(200, 200)))
		w.detailPanel = nil
		w.rowHeader = nil
		main.AddChild(w.logPanel, w.muted("交互式 Shell  ·  断线后手动重连"), row(), row())
	} else {
		changed := func(string) { p.dirty = true; p.editAt = time.Now() }
		w.filterInput = w.input(p.state.Filter, "筛选日志，例如  package:com.example  level:WARN", changed, func() { w.apply(p) })
		w.searchInput = w.input(p.state.Search, "在结果中查找…   Ctrl+F", changed, func() { w.apply(p) })
		caseKind, regexKind := "quiet", "quiet"
		if p.caseSensitive {
			caseKind = "active"
		}
		if p.regex {
			regexKind = "active"
		}
		query := column(w.inputRow("过滤", w.filterInput, w.action("应用", "primary", func() { w.apply(p) }), w.action("条件", "quiet", w.fieldHints), w.action("收藏", "quiet", w.filterHistory)), w.inputRow("查找", w.searchInput, w.action("Aa", caseKind, func() { p.caseSensitive = !p.caseSensitive; w.apply(p); w.rebuild() }), w.action(".*", regexKind, func() { p.regex = !p.regex; w.apply(p); w.rebuild() }), w.action("↑", "quiet", func() { w.nextMatch(true) }), w.action("↓", "quiet", func() { w.nextMatch(false) })))
		main.AddChild(w.inset(query, 12, c.surface))
		w.resultLabel = w.muted("准备读取日志")
		pause := "暂停"
		if p.paused {
			pause = "继续"
		}
		followKind := "quiet"
		if p.follow {
			followKind = "active"
		}
		w.pauseButton = w.action(pause, "quiet", w.pause)
		w.followButton = w.action("跟随", followKind, func() {
			p.keepSelectionVisible = false
			p.follow = !p.follow
			if p.follow {
				p.scroll = max(len(p.result.Rows)-w.visibleRows(), 0)
			}
		})
		w.recordButton = w.action("录制", "quiet", w.record)
		w.followActive = !p.follow
		w.recordActive = true
		clearButton := w.action("清空", "quiet", w.clearView)
		clearButton.GetWidget().Disabled = p.offline != nil
		mergeKind := "quiet"
		if p.state.MergeLines {
			mergeKind = "active"
		}
		main.AddChild(stretchRow(row(w.label("日志"), w.resultLabel), row(w.action("合并多行", mergeKind, w.toggleMergeLines), clearButton, w.pauseButton, w.followButton, w.recordButton, w.action("导出", "quiet", w.export), w.action("更多", "quiet", w.logMenu))))
		w.rowHeader = widget.NewContainer(widget.ContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(200, 30)), widget.ContainerOpts.BackgroundImage(uiimage.NewNineSliceColor(c.surface)))
		main.AddChild(w.rowHeader)
		w.logPanel = widget.NewContainer(widget.ContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(200, 90)))
		wrapKind := "quiet"
		if w.config.Wrap {
			wrapKind = "active"
		}
		w.copyDetailButton = w.action("复制选中", "quiet", w.copyDetailSelection)
		header := stretchRow(row(w.muted("日志详情"), w.muted("拖选文字 · Ctrl/Cmd+C 复制")), row(w.action("换行", wrapKind, func() {
			w.syncInputs()
			w.config.Wrap = !w.config.Wrap
			p.detailScroll, p.detailX = 0, 0
			w.rebuild()
			w.save()
		}), w.copyDetailButton, w.action("复制全部", "quiet", w.copySelected), w.action("上下文", "quiet", w.context)))
		w.detailPanel = widget.NewContainer(widget.ContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(200, minDetailBody)))
		group := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, true}), widget.GridLayoutOpts.Spacing(0, 8))))
		group.AddChild(header, w.detailPanel)
		w.detailSplitter = widget.NewContainer(widget.ContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(0, detailDividerHeight), w.hoverCursor(ebiten.CursorShapeNSResize)))
		w.detailArea = widget.NewContainer(widget.ContainerOpts.Layout(&detailSplitLayout{app: w}))
		w.detailArea.AddChild(w.logPanel, w.detailSplitter, group)
		main.AddChild(w.detailArea)
		if p.offline != nil {
			main.AddChild(stretchRow(w.muted("离线文件 · 查询完整来源"), row(w.action("上一页", "quiet", func() {
				if p.offset > 0 {
					p.offset = max(p.offset-2000, 0)
					p.scroll = 0
					w.refresh(p, true)
				}
			}), w.action("下一页", "quiet", func() {
				if p.offset+2000 < p.result.Total {
					p.offset += 2000
					p.scroll = 0
					w.refresh(p, true)
				}
			}))))
		} else {
			main.AddChild(w.muted("拖动表头分隔线调列宽  ·  ↑ / ↓ 选择日志  ·  Shift + 滚轮横向浏览  ·  F3 查找"))
		}
	}
	workspace.AddChild(w.inset(main, 14, c.bg))
	root.AddChild(workspace)
	w.statusLabel = w.statusText(p.status)
	root.AddChild(w.inset(w.statusLabel, 6, c.sidebar))
	w.updateChrome()
}
func (w *app) sidebar(p *page) *widget.Container {
	c := w.colors()
	content := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, false, false, false, false, false, false, false, false, false, true, false}), widget.GridLayoutOpts.Spacing(0, 12))))
	content.AddChild(w.muted("工作区 / 来源"))
	w.sourceLabel = w.label("Android 设备")
	w.sourceLabel.MaxWidth = 192
	w.connectionLabel = w.muted("● 未连接")
	content.AddChild(column(w.sourceLabel, w.connectionLabel))
	if p.shell != nil {
		content.AddChild(w.muted(p.state.Serial), w.action("切换到日志", "quiet", func() {
			for i, tab := range w.pages {
				if tab.shell == nil {
					w.selectPage(i)
					return
				}
			}
		}), w.muted("固定设备连接"), w.muted("切换日志页不会改变终端目标"), row(), row(), row(), row(), row(), row(), w.action("快捷键与帮助", "quiet", w.help))
	} else {
		w.serialInput = w.input(p.state.Serial, "选择设备或输入序列号", nil, nil)
		w.appInput = w.input(p.state.Package, "全部应用", func(string) { p.dirty = true; p.editAt = time.Now() }, func() { w.apply(p) })
		w.buffersInput = w.input(p.state.Buffers, "main,system,crash", nil, nil)
		if p.state.File != "" {
			name := w.label(filepath.Base(p.state.File))
			name.MaxWidth = 192
			pathLabel := w.muted(p.state.File)
			pathLabel.MaxWidth = 192
			content.AddChild(name, pathLabel, w.action("重新选择文件", "quiet", func() { w.browse("file", func(path string) { w.openOffline(p, path) }) }), w.muted("应用 / 进程"), w.appInput, w.action("清除应用限制", "quiet", func() { w.appInput.SetText(""); w.apply(p) }), w.muted("分析范围"), w.muted("完整文件 · 分页浏览"), row(), w.action("快捷键与帮助", "quiet", w.help))
		} else {
			content.AddChild(w.serialInput, w.action("选择设备", "quiet", w.chooseDevice))
			w.startButton = w.action("开始采集", "primary", func() {
				if p.collector != nil {
					w.stop(p)
				} else {
					w.start(p)
				}
			})
			w.startActive = false
			content.AddChild(w.startButton)
			content.AddChild(w.muted("应用 / 进程"), w.appInput, w.action("选择应用", "quiet", w.chooseApp), w.muted("日志缓冲区"), w.buffersInput, row(), w.action("快捷键与帮助", "quiet", w.help))
		}

	}
	box := w.inset(content, 16, c.sidebar)
	box.GetWidget().MinWidth = 224
	box.GetWidget().LayoutData = widget.GridLayoutData{MaxWidth: 224}
	return box
}
func (w *app) tabStrip() *widget.Container {
	tabs := row()
	for i, p := range w.pages {
		i := i
		kind := "quiet"
		if i == w.selected {
			kind = "active"
		}
		name := p.state.Name
		if p.shell != nil {
			name = "终端"
		} else if p.offline != nil {
			name = "离线 · " + name
		}
		runes := []rune(name)
		if len(runes) > 18 {
			name = string(runes[:18]) + "…"
		}
		fill := w.colors().surface
		if kind == "active" {
			fill = w.colors().accentSoft
		}
		tab := widget.NewContainer(widget.ContainerOpts.BackgroundImage(w.skin(fill, fill, 6)), widget.ContainerOpts.Layout(widget.NewRowLayout(widget.RowLayoutOpts.Direction(widget.DirectionHorizontal))))
		title := w.action(name, kind, func() {
			if !w.tabDrag.dragging && !w.tabDrag.canceled {
				w.selectPage(i)
			}
		})
		tab.AddChild(title, w.action("×", kind, func() { w.closeTab(p) }))
		w.tabWidgets = append(w.tabWidgets, tabWidget{page: p, title: title.GetWidget(), body: tab.GetWidget()})
		tabs.AddChild(tab)
	}
	scrollLeft := 0.0
	if w.tabScroll != nil {
		scrollLeft = w.tabScroll.ScrollLeft
	}
	w.tabScroll = w.scrollBox(tabs, 38)
	w.tabScroll.ScrollLeft = scrollLeft
	return stretchRow(w.tabScroll, row(w.action("+", "quiet", func() { w.syncInputs(); w.newPage(core.TabState{Name: "Logcat"}); w.rebuild(); w.save() }), w.action("标签", "quiet", w.tabMenu)))
}
func (w *app) openFile() {
	w.browse("file", func(path string) {
		w.syncInputs()
		p := w.newPage(core.TabState{Name: "离线", File: path})
		w.rebuild()
		w.openOffline(p, path)
	})
}
func (w *app) tabMenu() {
	w.show("管理当前标签", column(w.button("复制标签", func() { w.closeModal(); w.duplicate() }), w.button("重命名", func() { w.closeModal(); w.rename() }), w.button("向左移动", func() { w.closeModal(); w.move(-1) }), w.button("向右移动", func() { w.closeModal(); w.move(1) }), w.action("关闭标签", "danger", func() { w.closeModal(); w.closePage() })))
}
func (w *app) logMenu() {
	p := w.active()
	if p == nil {
		return
	}
	w.show("日志操作", column(w.button("跳到时间…", func() { w.closeModal(); w.jumpTime() }), w.button("取消当前查询", func() {
		w.closeModal()
		if p.cancel != nil {
			p.cancel()
		}
		p.job++
		p.busy = false
		p.status = "查询已取消"
	}), w.button("停止当前标签采集", func() { w.closeModal(); w.stop(p) }), w.muted("设备与共享会话"), w.action("停止共享采集…", "danger", func() { w.closeModal(); w.stopShared() }), w.action("清空设备缓冲区…", "danger", func() { w.closeModal(); w.clearDevice() })))
}
func (w *app) updateChrome() {
	p := w.active()
	if p == nil {
		return
	}
	update := func(label *widget.Text, value string) {
		if label != nil && label.Label != value {
			label.Label = value
			w.ui.Container.RequestRelayout()
		}
	}
	if w.sourceLabel != nil {
		value := "Android 设备"
		if p.offline != nil {
			value = "本地日志文件"
		} else {
			for _, d := range w.devices {
				if d.Serial == p.state.Serial {
					value = d.Name
					break
				}
			}
		}
		update(w.sourceLabel, value)
	}
	if p.shell == nil {
		scope := "实时"
		if p.offline != nil {
			scope = "离线"
		}
		grouped := ""
		if p.state.MergeLines {
			grouped = fmt.Sprintf(" · %s 组", number(len(p.result.Rows)))
			if p.offline != nil {
				grouped = fmt.Sprintf(" · 本页 %s 组", number(len(p.result.Rows)))
			}
		}
		update(w.resultLabel, fmt.Sprintf("%s · %s 条%s · %s 处匹配", scope, number(p.result.Total), grouped, number(p.result.SearchCount)))
	}
	if w.connectionLabel != nil {
		state, fg := "● 未连接", w.colors().muted
		if p.offline != nil {
			state = "● 离线文件"
		} else if p.shell != nil {
			state = "● 设备终端"
		} else if p.collector != nil {
			state = "● 连接中"
			if strings.HasPrefix(p.collector.Status(), "采集中") {
				state, fg = "● 采集中", w.colors().success
			}
		}
		w.connectionLabel.SetColor(fg)
		update(w.connectionLabel, state)
	}
	if w.startButton != nil && w.startActive != (p.collector != nil) {
		w.startActive = p.collector != nil
		if w.startActive {
			w.startButton.SetText("停止采集")
		} else {
			w.startButton.SetText("开始采集")
		}
	}
	if w.pauseButton != nil {
		w.pauseButton.GetWidget().Disabled = p.collector == nil
	}
	if w.copyDetailButton != nil {
		start, end := p.detailSelection.bounds()
		w.copyDetailButton.GetWidget().Disabled = p.selected < 0 || p.selected >= len(p.result.Rows) || end <= start || p.detailCache.entry == nil || end > len(p.detailCache.entry.Raw)
	}
	if w.recordButton != nil {
		w.recordButton.GetWidget().Disabled = p.collector == nil
		recording := p.collector != nil && p.collector.IsRecording()
		if w.recordActive != recording {
			w.recordActive = recording
			if recording {
				w.recordButton.SetText("● 录制中")
			} else {
				w.recordButton.SetText("录制")
			}
		}
	}
	if w.followButton != nil && w.followActive != p.follow {
		w.followActive = p.follow
		body := w.colors().surface
		border := body
		if p.follow {
			body = w.colors().accentSoft
			border = body
			w.followButton.SetText("跟随中")
		} else {
			w.followButton.SetText("跟随")
		}
		w.followButton.SetImage(&widget.ButtonImage{Idle: w.skin(body, border, 6), Hover: w.skin(w.colors().selection, w.colors().line, 6), Pressed: w.skin(w.colors().accentSoft, w.colors().accent, 6)})
		w.ui.Container.RequestRelayout()
	}
	update(w.statusLabel, p.status)
}
func number(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func (w *app) scrollBox(content widget.PreferredSizeLocateableWidget, height int) *widget.ScrollContainer {
	box := widget.NewScrollContainer(widget.ScrollContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(240, height), widget.WidgetOpts.LayoutData(widget.GridLayoutData{MaxHeight: height})), widget.ScrollContainerOpts.Content(content), widget.ScrollContainerOpts.StretchContentWidth(), widget.ScrollContainerOpts.Image(&widget.ScrollContainerImage{Idle: uiimage.NewNineSliceColor(w.background()), Mask: uiimage.NewNineSliceColor(w.background())}))
	box.GetWidget().ScrolledEvent.AddHandler(func(value any) {
		a := value.(*widget.WidgetScrolledEventArgs)
		cw, ch := content.PreferredSize()
		rect := box.GetWidget().Rect
		if ch > rect.Dy() {
			box.ScrollTop -= a.Y * 40 / float64(ch-rect.Dy())
		}
		if cw > rect.Dx() {
			delta := a.X
			if ch <= rect.Dy() {
				delta += a.Y
			}
			box.ScrollLeft -= delta * 40 / float64(cw-rect.Dx())
		}
	})
	return box
}
func (w *app) show(title string, content *widget.Container) { w.showDialog(title, content, nil) }
func (w *app) showDialog(title string, content *widget.Container, primary *widget.Button) {
	w.profileScene = title
	if w.removeModal != nil {
		w.removeModal()
		w.removeModal = nil
	}
	w.modal = true
	w.terminalFocus = false
	cw, ch := content.PreferredSize()
	const padding, spacing = 24, 16
	header := stretchRow(w.heading(title), w.action("×", "quiet", w.closeModal))
	footer := row(w.action("关闭", "quiet", w.closeModal))
	if primary != nil {
		footer.AddChild(primary)
	}
	hint := w.muted("Esc 关闭")
	footerRow := stretchRow(hint, footer)
	headerWidth, headerHeight := header.PreferredSize()
	footerWidth, footerHeight := footerRow.PreferredSize()
	width := min(w.width-80, max(520, min(max(cw, headerWidth, footerWidth)+2*padding, 860)))
	if title == "偏好设置" {
		width = min(w.width-80, 720)
	}
	// Measure the chrome instead of subtracting a guessed fixed height: button
	// padding and font metrics can otherwise squeeze the body's last row.
	chromeHeight := 2*padding + 2*spacing + headerHeight + footerHeight
	bodyLimit := min(510, max(1, w.height-40-chromeHeight))
	bodyHeight := min(max(ch, 2*controlHeight+12), bodyLimit)
	if ch > bodyHeight {
		hint.Label = "滚轮浏览更多 · Esc 关闭"
	}
	height := bodyHeight + chromeHeight
	container := widget.NewContainer(widget.ContainerOpts.BackgroundImage(w.skin(w.colors().surface, w.colors().line, 10)), widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, true, false}), widget.GridLayoutOpts.Padding(widget.NewInsetsSimple(padding)), widget.GridLayoutOpts.Spacing(0, spacing))))
	var body widget.PreferredSizeLocateableWidget = w.scrollBox(content, bodyHeight)
	if title == "版本更新" {
		// Only release notes scroll; version, status and actions stay in place.
		body = content
	}
	container.AddChild(header, body, footerRow)
	left, top := (w.width-width)/2, (w.height-height)/2
	window := widget.NewWindow(widget.WindowOpts.Contents(container), widget.WindowOpts.Modal(), widget.WindowOpts.BlockLower(true), widget.WindowOpts.Location(image.Rect(left, top, left+width, top+height)))
	w.modalWindow = window
	w.removeModal = w.ui.AddWindow(window)
}
func (w *app) closeModal() {
	back := w.modalReturn
	w.modalReturn = nil
	w.profileScene = ""
	if w.removeModal != nil {
		w.removeModal()
		w.removeModal = nil
	}
	w.modal = false
	w.modalWindow = nil
	w.ui.ClearFocus()
	if back != nil {
		back()
	}
}

func (w *app) showNestedDialog(title string, content *widget.Container) {
	parent, parentScene, parentReturn := w.modalWindow, w.profileScene, w.modalReturn
	w.show(title, content)
	if parent != nil {
		w.modalReturn = func() {
			w.modal, w.modalWindow, w.profileScene = true, parent, parentScene
			w.modalReturn = parentReturn
			w.removeModal = w.ui.AddWindow(parent)
			w.ui.Container.RequestRelayout()
			w.renderDirty = true
		}
	}
}
func (w *app) message(title, message string) {
	label := w.label(message)
	label.MaxWidth = 850
	w.show(title, column(label))
}
func (w *app) confirm(title, message string, yes func()) {
	label := w.label(message)
	label.MaxWidth = 850
	w.show(title, column(label, w.button("确认", func() { w.closeModal(); yes() }), w.button("取消", w.closeModal)))
}
func (w *app) prompt(title, initial string, apply func(string)) {
	entry := w.input(initial, "", nil, nil)
	button := w.button("确认", func() { s := entry.GetText(); w.closeModal(); apply(s) })
	w.show(title, column(entry, button))
	w.ui.SetFocusedWidget(entry)
}
func (w *app) choices(title string, values []string, apply func(string)) {
	box := column()
	search := w.input("", "筛选选项", nil, nil)
	list := column()
	var owner *widget.Window
	refresh := func(value string) {
		list.RemoveChildren()
		for _, s := range values {
			s := s
			if !strings.Contains(strings.ToLower(s), strings.ToLower(value)) {
				continue
			}
			list.AddChild(w.button(s, func() { w.closeModal(); apply(s) }))
		}
		list.RequestRelayout()
		box.RequestRelayout()
		if owner != nil && w.modalWindow == owner {
			owner.RequestRelayout()
			w.renderDirty = true
		}
	}
	search.ChangedEvent.AddHandler(func(a any) { refresh(a.(*widget.TextInputChangedEventArgs).InputText) })
	refresh("")
	box.AddChild(search, list)
	w.show(title, box)
	owner = w.modalWindow
}
func (w *app) fieldHints() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	fields := []string{"tag:", "package:", "process:", "message:", "line:", "level:", "age:", "pid:", "tid:", "after:", "before:"}
	w.choices("插入过滤字段", fields, func(field string) {
		value := strings.TrimSpace(w.filterInput.GetText())
		if value != "" {
			value += " "
		}
		w.filterInput.SetText(value + field)
		w.ui.SetFocusedWidget(w.filterInput)
	})
}
func (w *app) chooseDevice() {
	var labels []string
	mapping := map[string]core.Device{}
	for _, d := range w.devices {
		label := fmt.Sprintf("%s · %s · %s · %s", d.Name, d.Serial, d.State, d.Connection)
		labels = append(labels, label)
		mapping[label] = d
	}
	w.choices("设备", labels, func(label string) {
		d := mapping[label]
		if d.State != "device" {
			w.message("设备不可用", "设备状态："+d.State+"；检查授权及连接。")
			return
		}
		p := w.active()
		if p != nil {
			w.stop(p)
			p.state.Serial = d.Serial
			w.serialInput.SetText(d.Serial)
			p.status = "已选设备，点击开始；旧结果来自之前的采集"
			w.save()
		}
	})
}
func (w *app) chooseApp() {
	p := w.active()
	if p == nil {
		return
	}
	w.syncInputs()
	client, serial := w.client, p.state.Serial
	if client == nil || serial == "" {
		w.message("应用列表", "先选择在线设备。")
		return
	}
	w.async(func() error {
		ctx, cancel := context.WithTimeout(w.ctx, 15*time.Second)
		defer cancel()
		packages, err := client.Packages(ctx, serial)
		if err == nil {
			w.post(func() {
				w.choices("选择完整包名", append([]string{"全部应用"}, packages...), func(pkg string) {
					if pkg == "全部应用" {
						pkg = ""
					}
					w.appInput.SetText(pkg)
					w.apply(p)
				})
			})
		}
		return err
	}, nil)
}
