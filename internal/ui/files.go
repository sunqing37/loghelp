package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ncruces/zenity"
)

func (w *app) dialogDirectory(initial string) string {
	home, _ := os.UserHomeDir()
	for _, path := range []string{initial, w.fileDirectory, w.config.Directory, home} {
		if path == "" {
			continue
		}
		path, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if info, err := os.Stat(path); err == nil {
			if !info.IsDir() {
				path = filepath.Dir(path)
			}
			return path + string(os.PathSeparator)
		}
	}
	return ""
}

func (w *app) browse(mode string, choose func(string)) {
	if mode == "directory" {
		w.selectDirectory(w.config.Directory, choose)
		return
	}
	initial := ""
	if p := w.active(); p != nil {
		initial = p.state.File
	}
	w.fileDialog("open", "打开日志文件", w.dialogDirectory(initial), zenity.FileFilters{
		{Name: "日志、压缩包与录制会话", Patterns: []string{"*.txt", "*.log", "*.jsonl", "*.json", "*.xlog", "*.zip", "*.tar", "*.gz", "*.tgz", "*.bz2", "*.tbz2", "*.zst"}, CaseFold: true},
		{Name: "所有文件", Patterns: []string{"*"}},
	}, choose)
}

func (w *app) selectDirectory(initial string, choose func(string)) {
	w.fileDialog("directory", "选择录制目录", w.dialogDirectory(initial), nil, choose)
}

func (w *app) selectADBFile(initial string, choose func(string)) {
	filters := zenity.FileFilters{}
	if runtime.GOOS == "windows" {
		filters = append(filters, zenity.FileFilter{Name: "可执行文件", Patterns: []string{"*.exe"}, CaseFold: true})
	}
	filters = append(filters, zenity.FileFilter{Name: "所有文件", Patterns: []string{"*"}})
	w.fileDialog("open", "选择 ADB 可执行文件", w.dialogDirectory(initial), filters, choose)
}

func (w *app) saveLogFile(choose func(string)) {
	name := "export-" + time.Now().Format("20060102-150405") + ".txt"
	w.fileDialog("save", "导出日志（.txt 文本 / .jsonl 结构化）", filepath.Join(w.dialogDirectory(""), name), zenity.FileFilters{
		{Name: "文本日志", Patterns: []string{"*.txt"}, CaseFold: true},
		{Name: "结构化日志", Patterns: []string{"*.jsonl"}, CaseFold: true},
		{Name: "所有文件", Patterns: []string{"*"}},
	}, choose)
}

func (w *app) fileDialog(mode, title, initial string, filters zenity.FileFilters, choose func(string)) {
	w.fileDialogPaths(mode, title, initial, filters, false, func(paths []string) {
		if len(paths) > 0 {
			choose(paths[0])
		}
	})
}

func (w *app) fileDialogPaths(mode, title, initial string, filters zenity.FileFilters, multiple bool, choose func([]string)) {
	if w.fileDialogOpen || w.ctx.Err() != nil {
		return
	}
	if !zenity.IsAvailable() {
		w.message("系统文件对话框不可用", "请安装 zenity、matedialog 或 qarma 后重试。")
		return
	}
	options := []zenity.Option{zenity.Title(title), zenity.Filename(initial), zenity.Modal()}
	options = append(options, nativeDialogOwner()...)
	if len(filters) > 0 {
		options = append(options, filters)
	}
	if mode == "directory" {
		options = append(options, zenity.Directory())
	}
	if mode == "save" {
		options = append(options, zenity.ConfirmOverwrite())
	}
	ctx, cancel := context.WithCancel(w.ctx)
	options = append(options, zenity.Context(ctx))
	w.fileDialogOpen = true
	w.terminalFocus = false
	w.ui.ClearFocus()
	go func() {
		defer cancel()
		var paths []string
		var err error
		if multiple {
			paths, err = zenity.SelectFileMultiple(options...)
		} else {
			var path string
			if mode == "save" {
				path, err = zenity.SelectFileSave(options...)
			} else {
				path, err = zenity.SelectFile(options...)
			}
			paths = []string{path}
		}
		w.post(func() {
			w.fileDialogOpen = false
			if errors.Is(err, zenity.ErrCanceled) || errors.Is(err, context.Canceled) || w.ctx.Err() != nil {
				return
			}
			if err != nil {
				w.message("系统文件对话框", err.Error())
				return
			}
			// Whitespace can be part of a valid Unix filename.
			if len(paths) == 0 {
				return
			}
			for _, path := range paths {
				if path == "" || strings.IndexByte(path, 0) >= 0 {
					return
				}
			}
			path := paths[0]
			if mode == "directory" {
				w.fileDirectory = path
			} else {
				w.fileDirectory = filepath.Dir(path)
			}
			choose(paths)
		})
	}()
}
