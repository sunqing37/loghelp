package ui

import (
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"loghelp/internal/core"
)

func (w *app) openLogPath(path string) {
	w.closeModal()
	w.syncInputs()
	p := w.newPage(core.TabState{Name: filepath.Base(path), File: path})
	w.rebuild()
	w.openOffline(p, path)
}

func (w *app) dropLogs() {
	dropped := ebiten.DroppedFiles()
	if dropped == nil || w.backgroundDiagnostic {
		return
	}
	if w.modal || w.fileDialogOpen {
		return
	}
	// Desktop entries expose their native absolute path; open the original file,
	// preserving large-file indexing and sibling manifest segment resolution.
	go func() {
		entries, err := fs.ReadDir(dropped, ".")
		paths := []string{}
		if err == nil {
			for _, entry := range entries {
				if p, ok := entry.(ebiten.AbsPather); ok {
					paths = append(paths, p.AbsPath())
				}
				if len(paths) == 10 {
					break
				}
			}
		}
		w.post(func() {
			if err != nil {
				w.message("拖入文件失败", err.Error())
				return
			}
			if len(paths) == 0 {
				w.message("拖入文件", "无法取得本地文件路径，请使用打开文件。")
				return
			}
			if w.modal || w.fileDialogOpen {
				w.message("拖入文件", "窗口状态已变化，请关闭弹窗后重新拖入。")
				return
			}
			for _, path := range paths {
				w.openLogPath(path)
			}
		})
	}()
}

func (w *app) fileMenu() {
	box := w.column(w.button("选择日志文件…", w.openFile), w.diagnosticText("可直接把日志文件或录制目录拖入主窗口，一次最多 10 个。最近打开仅保存成功打开的路径。"))
	for _, path := range w.config.RecentFiles {
		path := path
		box.AddChild(w.button(w.wirelessText(path, w.wirelessContentWidth()-w.px(28)), func() { w.openLogPath(path) }))
	}
	box.AddChild(w.button("清空最近打开", func() { w.config.RecentFiles = nil; w.save(); w.fileMenu() }), w.button("排查方案", w.workspaces))
	w.show("文件与排查方案", box)
}

func (w *app) workspaces() {
	box := w.column(w.diagnosticText("方案保存应用、过滤、搜索、设备/文件路径、多个日志标签和显示列。恢复时新增标签，保留当前采集与录制；实时标签需手动开始采集。"), w.button("保存当前排查方案", func() {
		if !w.configWritable {
			w.message("无法保存方案", "配置当前不可写。")
			return
		}
		w.syncInputs()
		var tabs []core.TabState
		for _, p := range w.pages {
			if p.shell == nil {
				t := p.state
				t.CaseSensitive = p.caseSensitive
				t.SearchRegex = p.regex
				tabs = append(tabs, t)
			}
		}
		if len(tabs) == 0 || len(tabs) > 20 {
			w.message("保存方案", "一个方案需包含 1–20 个日志标签。")
			return
		}
		columns := append([]string(nil), w.config.Columns...)
		widths := maps.Clone(w.config.ColumnWidths)
		wrap := w.config.Wrap
		w.prompt("方案名称（同名覆盖）", "", func(name string) {
			name = strings.TrimSpace(name)
			if name == "" || len([]rune(name)) > 60 {
				w.message("方案名称", "请输入 1–60 个字符。")
				return
			}
			plan := core.Workspace{Name: name, Tabs: tabs, Columns: columns, ColumnWidths: widths, Wrap: wrap}
			for i, p := range w.config.Workspaces {
				if p.Name == name {
					w.config.Workspaces[i] = plan
					w.save()
					return
				}
			}
			if len(w.config.Workspaces) >= 20 {
				w.message("方案已满", "最多 20 个方案，请先删除旧方案。")
				return
			}
			w.config.Workspaces = append(w.config.Workspaces, plan)
			w.save()
		})
	}))
	for i, plan := range w.config.Workspaces {
		i, plan := i, plan
		box.AddChild(w.diagnosticText(fmt.Sprintf("%s · %d 个标签", plan.Name, len(plan.Tabs))), w.row(w.button("恢复", func() {
			if len(plan.Tabs) > 20 || len(w.pages)+len(plan.Tabs) > 40 {
				w.message("标签过多", "恢复后最多 40 个标签，请先关闭部分标签。")
				return
			}
			w.closeModal()
			w.syncInputs()
			w.config.Columns = append([]string(nil), plan.Columns...)
			w.config.ColumnWidths = maps.Clone(plan.ColumnWidths)
			w.config.Wrap = plan.Wrap
			for _, t := range plan.Tabs {
				p := w.newPage(t)
				if t.File != "" {
					w.openOffline(p, t.File)
				}
			}
			w.rebuild()
			w.save()
		}), w.button("删除", func() {
			w.config.Workspaces = append(w.config.Workspaces[:i], w.config.Workspaces[i+1:]...)
			w.save()
			w.workspaces()
		})))
	}
	w.show("排查方案", box)
}
