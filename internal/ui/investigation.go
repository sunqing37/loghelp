package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
	"loghelp/internal/support"
)

type logSource struct {
	offline *core.Offline
	rows    []*core.Entry
	scan    core.EntryScan
	scope   string
}

func (w *app) investigationSource(p *page) logSource {
	w.syncInputs()
	s := logSource{offline: p.offline, scope: "当前过滤结果 · 打开时的来源快照"}
	if p.offline == nil {
		s.rows = p.stopped
		if p.paused {
			s.rows = p.frozen
		} else if p.collector != nil {
			s.rows, _, _ = p.collector.Ring.Snapshot()
		}
		s.scope = "当前过滤结果 · 当前缓存/暂停快照，可能已有淘汰"
	}
	q, pkg, clear, anchor := p.query, p.state.Package, p.clearSeq, time.Now()
	if s.offline != nil {
		anchor = s.offline.Anchor
	}
	s.scan = func(ctx context.Context, visit func(*core.Entry) bool) error {
		accept := func(e *core.Entry) bool {
			if ctx.Err() != nil {
				return false
			}
			if (s.offline != nil || e.Seq > clear) && q.Match(e, anchor) && (pkg == "" || pkg == e.Package) {
				return visit(e)
			}
			return true
		}
		if s.offline != nil {
			return s.offline.Scan(ctx, accept, nil)
		}
		for _, e := range s.rows {
			if !accept(e) {
				break
			}
		}
		return ctx.Err()
	}
	return s
}

func (w *app) jumpInsight(s logSource, seq uint64) {
	w.closeModal()
	w.syncInputs()
	p := w.newPage(core.TabState{Name: "分析定位"})
	p.follow = false
	if s.offline != nil {
		p.offline = s.offline
		p.state.File = s.offline.Path
		p.offset = int(seq - 1)
		p.selectFirst = true
	} else {
		p.stopped = s.rows
		p.selectionSeq = seq
		p.keepSelectionVisible = true
	}
	w.rebuild()
	w.refresh(p, true)
}

func (w *app) logInsights(eventsOnly bool) {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	s := w.investigationSource(p)
	ctx, job, _ := w.beginPageJob(p, "分析日志", 0)
	finish := w.showAnalysisProgress(p, ctx, job, "分析日志")
	go func() {
		r, err := core.AnalyzeLogs(ctx, s.scan)
		w.post(func() {
			if !finish() {
				return
			}
			if err != nil {
				w.message("分析失败", err.Error())
				return
			}
			w.showInsights(s, r, eventsOnly, 0)
		})
	}()
}

func (w *app) showInsights(s logSource, r core.LogOverview, eventsOnly bool, page int) {
	box := w.column(w.diagnosticText(s.scope), w.label(fmt.Sprintf("共 %d 条 · ERROR/FATAL %d 条 · 无设备时间/采集事件 %d 条", r.Total, r.Errors, r.Untimed)))
	box.AddChild(w.button("查看概览 / 异常线索", func() { w.showInsights(s, r, !eventsOnly, 0) }))
	const size = 40
	if eventsOnly {
		box.AddChild(w.diagnosticText(fmt.Sprintf("崩溃、ANR、退出仅为关键词线索，不代表根因。最多显示 500 项，额外 %d 项未列出。点击打开独立未过滤来源，可继续查看上下文。", r.ExtraEvents)))
		for _, e := range r.Events[min(page*size, len(r.Events)):min((page+1)*size, len(r.Events))] {
			e := e
			box.AddChild(w.button(w.wirelessText(fmt.Sprintf("%s · %s · %s\n%s", e.Time.Format("01-02 15:04:05.000"), e.Kind, e.App, e.Summary), w.wirelessContentWidth()-w.px(28)), func() { w.jumpInsight(s, e.Seq) }))
		}
		if len(r.Events) == 0 {
			box.AddChild(w.label("未识别到异常线索；这不表示日志中没有问题。"))
		}
	} else {
		tags := make([]string, 0, len(r.Tags))
		for tag := range r.Tags {
			tags = append(tags, tag)
		}
		sort.Slice(tags, func(i, j int) bool {
			if r.Tags[tags[i]] == r.Tags[tags[j]] {
				return tags[i] < tags[j]
			}
			return r.Tags[tags[i]] > r.Tags[tags[j]]
		})
		if page == 0 {
			box.AddChild(w.heading("最频繁 TAG（前 10）"))
			for _, tag := range tags[:min(10, len(tags))] {
				box.AddChild(w.diagnosticText(fmt.Sprintf("%s · %d 条", tag, r.Tags[tag])))
			}
			if r.OtherTags > 0 {
				box.AddChild(w.label(fmt.Sprintf("其他未单独统计 TAG：%d 条", r.OtherTags)))
			}
		}
		box.AddChild(w.diagnosticText(fmt.Sprintf("时间分布 · 每格 %d 秒（短来源为每秒）；点击定位该格首条。设备时间回拨可能影响分布。", r.BucketSeconds)))
		peak := 1
		for _, b := range r.Buckets {
			peak = max(peak, b.Count)
		}
		for _, b := range r.Buckets[min(page*size, len(r.Buckets)):min((page+1)*size, len(r.Buckets))] {
			b := b
			label := fmt.Sprintf("%s  %s  %d 条 / 错误 %d", time.Unix(b.Second, 0).Format("01-02 15:04:05"), strings.Repeat("▏", max(1, b.Count*24/peak)), b.Count, b.Errors)
			box.AddChild(w.button(label, func() { w.jumpInsight(s, b.FirstSeq) }))
		}
	}
	n := len(r.Buckets)
	if eventsOnly {
		n = len(r.Events)
	}
	box.AddChild(w.label(fmt.Sprintf("第 %d / %d 页", page+1, max(1, (n+size-1)/size))))
	if page > 0 {
		box.AddChild(w.button("上一页", func() { w.showInsights(s, r, eventsOnly, page-1) }))
	}
	if (page+1)*size < n {
		box.AddChild(w.button("下一页", func() { w.showInsights(s, r, eventsOnly, page+1) }))
	}
	w.show("日志概览与异常导航", box)
}

func (w *app) viewJSON() {
	p := w.active()
	if selectedEntry(p) == nil {
		w.message("JSON 查看", "请先选择包含 JSON 的日志。")
		return
	}
	raw := p.detailEntry(p.selected).Raw
	n, err := core.ParseLogJSON(raw)
	if err != nil {
		w.message("JSON 查看", err.Error())
		return
	}
	expanded := map[string]bool{"": true}
	var show func(int)
	show = func(page int) {
		box := w.column(w.row(w.button("原文", func() { w.previewReportFile(support.ReportFile{Name: "原始日志", Data: []byte(raw)}) }), w.button("格式化全文", func() { w.previewReportFile(support.ReportFile{Name: "JSON", Data: []byte(n.Pretty())}) }), w.button("复制 JSON", func() {
			if err := clipboard.WriteAll(n.Pretty()); err != nil {
				w.message("复制失败", err.Error())
			}
		})))
		type row struct {
			n     *core.JSONNode
			depth int
		}
		var visible []row
		var walk func(*core.JSONNode, int)
		walk = func(n *core.JSONNode, d int) {
			visible = append(visible, row{n, d})
			if expanded[n.Path] {
				for _, child := range n.Children {
					walk(child, d+1)
				}
			}
		}
		walk(n, 0)
		page = min(page, max(0, (len(visible)-1)/40))
		for _, r := range visible[page*40 : min((page+1)*40, len(visible))] {
			r := r
			prefix := ""
			if len(r.n.Children) > 0 {
				prefix = "+ "
				if expanded[r.n.Path] {
					prefix = "− "
				}
			}
			value := shortText(r.n.CopyValue(), 80)
			if len(r.n.Children) > 0 {
				value = fmt.Sprintf("%d 项", len(r.n.Children))
			}
			label := strings.Repeat("  ", min(r.depth, 8)) + prefix + shortText(r.n.Name, 50) + ": " + value
			var expand *widget.Button
			expand = w.button(w.wirelessText(label, w.wirelessContentWidth()-w.px(130)), func() {
				if len(r.n.Children) > 0 {
					expanded[r.n.Path] = !expanded[r.n.Path]
					show(page)
				} else {
					w.previewReportFile(support.ReportFile{Name: r.n.Path, Data: []byte(r.n.CopyValue())})
				}
			})
			box.AddChild(w.row(expand, w.button("复制值", func() {
				if err := clipboard.WriteAll(r.n.CopyValue()); err != nil {
					w.message("复制失败", err.Error())
				}
			})))
		}
		box.AddChild(w.label(fmt.Sprintf("第 %d / %d 页 · 点击层级折叠/展开；叶节点可查看完整值", page+1, max(1, (len(visible)+39)/40))))
		if page > 0 {
			box.AddChild(w.button("上一页", func() { show(page - 1) }))
		}
		if (page+1)*40 < len(visible) {
			box.AddChild(w.button("下一页", func() { show(page + 1) }))
		}
		w.show("JSON 格式化查看", box)
	}
	show(0)
}
