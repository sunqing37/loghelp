package ui

import (
	"io/fs"
	"path/filepath"

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
	box.AddChild(w.button("清空最近打开", func() { w.config.RecentFiles = nil; w.save(); w.fileMenu() }))
	w.show("文件", box)
}
