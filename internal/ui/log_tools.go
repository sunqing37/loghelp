package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"loghelp/internal/core"
)

type timeAnchor struct {
	time       time.Time
	serial     string
	generation uint64
}

func selectedEntry(p *page) *core.Entry {
	if p == nil || p.shell != nil || p.selected < 0 || p.selected >= len(p.result.Rows) {
		return nil
	}
	return p.result.Rows[p.selected]
}

// Quote for the query lexer (which only unescapes quotes and backslashes).
func quoteFilter(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func (w *app) quickFilter() {
	p := w.active()
	e := selectedEntry(p)
	if e == nil {
		w.message("快速过滤", "先选择一条日志，或在详情中拖选文字。")
		return
	}
	value := w.selectedDetailText(p)
	box := w.column(w.muted("新条件与现有过滤按 AND 组合；查找按普通文本匹配。"))
	add := func(label, field, value string, exact bool) {
		if value == "" {
			return
		}
		term := field + ":" + quoteFilter(value)
		if exact {
			term = field + "~:" + quoteFilter("^"+regexp.QuoteMeta(value)+"$")
		}
		if strings.Contains(value, "\t") {
			pattern := strings.ReplaceAll(regexp.QuoteMeta(value), "\t", `\t`)
			if exact {
				pattern = "^" + pattern + "$"
			}
			term = field + "~:" + quoteFilter(pattern)
		}
		box.AddChild(w.row(w.button("包含"+label, func() { w.applyQuickFilter(p, term) }), w.button("排除"+label, func() { w.applyQuickFilter(p, "-"+term) })))
	}
	if value != "" {
		if strings.ContainsAny(value, "\r\n") || len(value) > 4096 {
			box.AddChild(w.muted("选中文字需为单行且不超过 4 KiB，请缩小选区。"))
		} else {
			preview := w.label("选中：" + shortText(value, 100))
			preview.MaxWidth = float64(w.px(700))
			box.AddChild(preview, w.button("查找选中文字", func() {
				w.closeModal()
				w.syncInputs()
				p.regex = false
				if strings.Contains(value, "\t") {
					p.regex = true
					value = strings.ReplaceAll(regexp.QuoteMeta(value), "\t", `\t`)
				}
				w.searchInput.SetText(value)
				w.apply(p)
				w.rebuild()
			}))
			add("选中文字", "line", value, false)
		}
	} else {
		box.AddChild(w.muted("拖选详情文字后，可直接查找、包含或排除该文字。"))
	}
	add(" TAG", "tag", e.Tag, true)
	if e.PID > 0 {
		add(" PID", "pid", strconv.Itoa(e.PID), false)
	}
	if e.TID > 0 {
		add(" TID", "tid", strconv.Itoa(e.TID), false)
	}
	add("应用", "package", e.Package, true)
	add("来源文件", "source", e.SourceFile, true)
	w.show("快速过滤", box)
}

func (w *app) applyQuickFilter(p *page, term string) {
	w.closeModal()
	if p.dead || w.active() != p {
		return
	}
	w.syncInputs()
	value := strings.TrimSpace(p.state.Filter)
	if value != "" {
		term = "(" + value + ") & (" + term + ")"
	}
	if _, err := core.CompileQuery(term, p.caseSensitive); err != nil {
		w.message("快速过滤未应用", "请先修正现有过滤："+err.Error())
		return
	}
	w.filterInput.SetText(term)
	w.apply(p)
}

func shortText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func (w *app) timeDifference() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	box := w.column(w.label(p.timeDifferenceText()), w.muted("按设备日志时间计算；合并组使用首条日志。"))
	if e := selectedEntry(p); e != nil && !e.Event && !e.Time.IsZero() {
		anchor := &timeAnchor{time: e.Time, serial: e.Serial, generation: e.Generation}
		box.AddChild(w.button("将当前日志设为起点", func() {
			p.timeAnchor = anchor
			w.closeModal()
			w.renderDirty = true
		}))
	}
	if p.timeAnchor != nil {
		box.AddChild(w.button("清除时间起点", func() { p.timeAnchor = nil; w.closeModal(); w.renderDirty = true }))
	}
	w.show("时间差", box)
}

func (p *page) timeDifferenceText() string {
	if p.timeAnchor == nil {
		return "时间差：未设置起点"
	}
	a := p.timeAnchor
	prefix := "起点 " + a.time.Format("15:04:05.000")
	e := selectedEntry(p)
	if e == nil {
		return prefix + " · 请选择终点日志"
	}
	if e.Event || e.Time.IsZero() {
		return prefix + " · 当前日志无有效设备时间"
	}
	if e.Serial != a.serial || e.Generation != a.generation {
		return prefix + " · 不同设备或采集代次，无法比较"
	}
	return fmt.Sprintf("%s · Δt %+.3f s", prefix, e.Time.Sub(a.time).Seconds())
}

func (w *app) addBookmark() {
	p := w.active()
	e := selectedEntry(p)
	if e == nil {
		w.message("书签", "先选择一条日志。合并组的书签定位到首条。")
		return
	}
	if !w.configWritable {
		w.message("书签", "当前配置不可写，无法保存书签。")
		return
	}
	b := core.NewBookmark(e)
	if p.offline != nil {
		b.Source = p.offline.Path
	}
	b.Paths = append([]string(nil), p.recordingPaths...)
	if p.collector != nil {
		b.Paths = append(b.Paths, p.collector.RecordingDirectories()...)
	}
	for _, existing := range w.config.Bookmarks {
		if existing.Source == b.Source && existing.Matches(e, p.offline == nil) {
			w.editBookmark(existing.ID, existing.Note)
			return
		}
	}
	if len(w.config.Bookmarks) >= 200 {
		w.message("书签已满", "最多保存 200 个书签，请先删除不再需要的书签。")
		return
	}
	w.prompt("添加书签备注", "", func(note string) {
		b.Note = shortText(strings.TrimSpace(note), 200)
		if b.Note == "" {
			b.Note = "未命名书签"
		}
		w.config.Bookmarks = append(w.config.Bookmarks, b)
		w.save()
	})
}

func (w *app) editBookmark(id, note string) {
	if !w.configWritable {
		w.message("书签", "当前配置不可写，无法修改书签。")
		return
	}
	w.prompt("编辑书签备注", note, func(value string) {
		value = shortText(strings.TrimSpace(value), 200)
		if value == "" {
			value = "未命名书签"
		}
		for i := range w.config.Bookmarks {
			if w.config.Bookmarks[i].ID == id {
				w.config.Bookmarks[i].Note = value
				break
			}
		}
		w.save()
	})
}

func (w *app) bookmarks() {
	box := w.column(w.button("为当前日志添加 / 编辑书签", w.addBookmark), w.muted("书签保存备注和定位信息；日志仍需在缓存或原文件中。"))
	for i := len(w.config.Bookmarks) - 1; i >= 0; i-- {
		b := w.config.Bookmarks[i]
		label := w.label(b.Note + " · " + b.Time.Format("2006-01-02 15:04:05.000") + "\n" + b.Preview)
		label.MaxWidth = float64(w.px(720))
		box.AddChild(label, w.row(w.button("定位", func() { w.locateBookmark(b) }), w.button("备注", func() { w.editBookmark(b.ID, b.Note) }), w.button("选择来源…", func() {
			w.closeModal()
			w.browse("file", func(path string) { w.openBookmark(b, path) })
		}), w.button("删除", func() {
			if !w.configWritable {
				w.message("书签", "当前配置不可写，无法删除书签。")
				return
			}
			for j := range w.config.Bookmarks {
				if w.config.Bookmarks[j].ID == b.ID {
					w.config.Bookmarks = append(w.config.Bookmarks[:j], w.config.Bookmarks[j+1:]...)
					break
				}
			}
			w.closeModal()
			w.save()
		})))
	}
	w.show("日志书签 · Ctrl/Cmd+B 添加", box)
}

func (w *app) locateBookmark(b core.Bookmark) {
	if b.Source != "" {
		w.openBookmark(b, b.Source)
		return
	}
	// A dedicated unfiltered snapshot leaves the user's current filter intact.
	for _, source := range w.pages {
		if source.shell != nil || source.offline != nil {
			continue
		}
		entries := source.stopped
		if source.paused {
			entries = source.frozen
		}
		for attempt := 0; attempt < 2; attempt++ {
			for _, e := range entries {
				if !b.Matches(e, true) {
					continue
				}
				w.closeModal()
				w.syncInputs()
				p := w.newPage(core.TabState{Name: "书签 · " + shortText(b.Note, 20)})
				p.stopped, p.recordingPaths = entries, b.Paths
				p.follow, p.keepSelectionVisible, p.selectionSeq = false, true, e.Seq
				w.rebuild()
				w.refresh(p, true)
				return
			}
			if source.collector == nil || attempt == 1 {
				break
			}
			entries, _, _ = source.collector.Ring.Snapshot()
		}
	}
	if len(b.Paths) == 1 {
		w.openBookmark(b, b.Paths[0])
		return
	}
	if len(b.Paths) > 1 {
		w.choices("选择关联录制", b.Paths, func(path string) { w.openBookmark(b, path) })
		return
	}
	w.message("书签来源不可用", "该日志已不在缓存中。请在书签列表使用“选择来源”打开包含它的录制或日志文件。备注仍保留。")
}

func (w *app) openBookmark(b core.Bookmark, path string) {
	w.closeModal()
	w.syncInputs()
	p := w.newPage(core.TabState{Name: "书签", File: path})
	w.rebuild()
	w.openOfflineTarget(p, path, nil, &b)
}
