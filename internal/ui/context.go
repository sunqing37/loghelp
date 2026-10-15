package ui

import (
	"fmt"
	"strconv"
	"strings"

	"loghelp/internal/core"
)

type contextTarget struct {
	entry         *core.Entry
	lastSeq       uint64
	before, after int
	source        *core.Offline
	collector     *core.Collector
	fallback      []*core.Entry
	paths         []string
}

func (w *app) context() {
	p := w.active()
	if p == nil || p.selected < 0 || p.selected >= len(p.result.Rows) {
		return
	}
	target := contextTarget{entry: p.result.Rows[p.selected], lastSeq: p.rowLastSeq(p.selected), before: w.config.ContextBefore, after: w.config.ContextAfter, source: p.offline, collector: p.collector, fallback: p.stopped, paths: append([]string(nil), p.recordingPaths...)}
	if target.collector != nil {
		target.paths = append(target.paths, target.collector.RecordingDirectories()...)
	}
	w.loadContext(p, target)
}

func (w *app) loadContext(p *page, target contextTarget) {
	if p.dead {
		return
	}
	ctx, job, progress := w.beginPageJob(p, "读取未过滤上下文", 0)
	finish := w.showAnalysisProgress(p, ctx, job, "读取未过滤上下文")
	go func() {
		var rows []*core.Entry
		var err error
		first := uint64(1)
		if target.entry.Seq > uint64(target.before) {
			first = target.entry.Seq - uint64(target.before)
		}
		last := target.lastSeq + uint64(target.after)
		if target.source != nil {
			rows, err = target.source.ContextProgress(ctx, target.entry.Seq, target.before, int(target.lastSeq-target.entry.Seq)+target.after, progress)
		} else {
			fallback := target.fallback
			if target.collector != nil {
				fallback, _, _ = target.collector.Ring.Snapshot()
			}
			for _, e := range fallback {
				if ctx.Err() != nil {
					err = ctx.Err()
					break
				}
				if e.Seq >= first && e.Seq <= last {
					rows = append(rows, core.ContextPreview(e))
				}
			}
		}
		w.post(func() {
			if !finish() {
				return
			}
			if err != nil {
				w.message("上下文读取失败", err.Error())
				return
			}
			w.showContext(p, target, rows, first, last, "")
		})
	}()
}

func (w *app) showContext(p *page, target contextTarget, rows []*core.Entry, first, last uint64, validation string) {
	before := w.contextCountInput("之前条数", target.before)
	after := w.contextCountInput("之后条数", target.after)
	content := w.column(w.inputRow("之前条数", before), w.inputRow("之后条数", after))
	content.AddChild(w.button("更新上下文", func() {
		b, errB := strconv.Atoi(strings.TrimSpace(before.GetText()))
		a, errA := strconv.Atoi(strings.TrimSpace(after.GetText()))
		if errB != nil || errA != nil || b < 0 || b > 500 || a < 0 || a > 500 {
			w.showContext(p, target, rows, first, last, "前后条数分别填写 0–500。")
			return
		}
		w.config.ContextBefore, w.config.ContextAfter = b, a
		w.save()
		target.before, target.after = b, a
		w.loadContext(p, target)
	}))
	if validation != "" {
		content.AddChild(w.label(validation))
	}
	found := false
	for _, e := range rows {
		if e.Seq == target.entry.Seq {
			found = true
			break
		}
	}
	if !found {
		content.AddChild(w.label("目标日志已不在当前来源范围，可能已被缓存淘汰。可打开录制文件定位原文。"))
	} else if len(rows) == 0 || rows[0].Seq > first || rows[len(rows)-1].Seq < last {
		content.AddChild(w.label("部分上下文超出当前来源范围，可能已被淘汰，或已到来源的起点/末尾。"))
	}
	if target.source == nil {
		paths := append([]string(nil), target.paths...)
		if target.collector != nil {
			paths = append(paths, target.collector.RecordingDirectories()...)
		}
		seen := map[string]bool{}
		for i := len(paths) - 1; i >= 0; i-- {
			path := paths[i]
			if seen[path] {
				continue
			}
			seen[path] = true
			label := w.muted(path)
			label.MaxWidth = float64(w.px(760))
			content.AddChild(label, w.button("打开此录制并定位目标", func() { w.openContextRecording(target.entry, path) }))
		}
		content.AddChild(w.button("选择其他录制文件并定位…", func() {
			w.closeModal()
			w.browse("file", func(path string) { w.openContextRecording(target.entry, path) })
		}))
	}
	content.AddChild(w.muted(fmt.Sprintf("前 %d 条 / 后 %d 条 · 共 %d 条 · 单条原文预览最多 4 KiB", target.before, target.after, len(rows))))
	for _, e := range rows {
		text := e.Raw
		if e.Seq >= target.entry.Seq && e.Seq <= target.lastSeq {
			text = "▶ " + text
		}
		label := w.label(text)
		label.MaxWidth = float64(w.px(760))
		content.AddChild(label)
	}
	w.show("未过滤上下文", content)
}

func (w *app) openContextRecording(target *core.Entry, path string) {
	w.closeModal()
	w.syncInputs()
	p := w.newPage(core.TabState{Name: "录制上下文", File: path})
	w.rebuild()
	w.openOfflineAt(p, path, target)
}
