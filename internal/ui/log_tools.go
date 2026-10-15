package ui

import (
	"loghelp/internal/core"
	"regexp"
	"strconv"
	"strings"
)

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
