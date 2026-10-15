package ui

import (
	"context"
	"fmt"
	"time"
)

func (w *app) cancelAnalysis() {
	if w.analysisCancel != nil {
		cancel := w.analysisCancel
		w.analysisCancel = nil
		cancel()
	}
	w.analysisPage, w.analysisLabel = nil, nil
}

// All helpers are started and completed on the UI thread. The progress
// callback alone runs in the worker and throttles delivery to the UI queue.
func (w *app) beginPageJob(p *page, title string, total int64) (context.Context, uint64, func(int64)) {
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	p.cancel = cancel
	p.job++
	job := p.job
	p.busy = true
	p.status = title + " · 可从更多菜单取消"
	var last time.Time
	progress := func(done int64) {
		if time.Since(last) < 150*time.Millisecond || ctx.Err() != nil {
			return
		}
		last = time.Now()
		w.post(func() {
			if p.dead || p.job != job || ctx.Err() != nil {
				return
			}
			if total > 0 {
				p.status = fmt.Sprintf("%s %.1f%% · 可取消", title, min(100, 100*float64(done)/float64(total)))
			} else {
				p.status = fmt.Sprintf("%s · 已读取 %.1f MiB · 可取消", title, float64(done)/(1<<20))
			}
			if w.analysisPage == p && w.analysisJob == job && w.analysisLabel != nil {
				w.analysisLabel.Label = p.status
			}
		})
	}
	return ctx, job, progress
}

func (w *app) showAnalysisProgress(p *page, ctx context.Context, job uint64, title string) func() bool {
	label := w.label(title + "，可取消；关闭窗口也会取消。")
	w.show(title, w.column(label, w.button("取消", w.closeModal)))
	owner := w.modalWindow
	w.analysisPage, w.analysisJob, w.analysisLabel = p, job, label
	w.analysisCancel = func() {
		if p.job == job {
			p.cancel()
			p.job++
			p.busy = false
			p.status = title + "已取消"
		}
	}
	return func() bool {
		if p.dead || p.job != job || ctx.Err() != nil || w.modalWindow != owner {
			return false
		}
		w.analysisCancel = nil
		p.cancel()
		p.busy = false
		w.closeModal()
		return true
	}
}

func (w *app) contextCountInput(label string, value int) *boundedInput {
	return w.input(fmt.Sprint(value), label+" 0–500", nil, nil)
}
