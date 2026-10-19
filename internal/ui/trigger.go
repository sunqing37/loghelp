package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"loghelp/internal/core"
)

func (w *app) triggerSettings() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	if p.triggerCancel != nil {
		w.show("关键词留证", w.column(w.diagnosticText(p.triggerStatus), w.button("刷新状态", w.triggerSettings), w.button("停止监听", func() {
			if p.triggerCancel != nil {
				p.triggerCancel()
			}
			p.triggerCancel = nil
			p.triggerID++
			p.triggerStatus = "已停止监听"
			w.closeModal()
		})))
		return
	}
	if p.collector == nil {
		w.message("关键词留证", "请先开始实时采集。")
		return
	}
	query := w.input("line:timeout", "使用过滤表达式，例如 line:timeout 或 level:ERROR", nil, nil)
	before, after, cooldown := w.input("50", "0–500", nil, nil), w.input("50", "0–500", nil, nil), w.input("30", "1–3600 秒", nil, nil)
	directory := w.input(w.config.Directory, "留证目录", nil, nil)
	box := w.column(w.diagnosticText("只监听启用后的新日志，不受暂停显示和当前过滤影响。前后按条数留证，后文最多等待 10 秒；缓存缺口会写入说明。默认文本脱敏，单次最多 20 个 ZIP；达到上限或写入失败即停止。停止监听会丢弃尚未完成的留证。"), w.inputRow("触发表达式", query), w.inputRow("之前条数", before), w.inputRow("之后条数", after), w.inputRow("冷却秒数", cooldown), w.inputRow("目录", directory), w.button("选择留证目录", func() { w.selectDirectory(directory.GetText(), func(path string) { directory.SetText(path) }) }))
	box.AddChild(w.button("开始监听", func() {
		q, err := core.CompileQuery(query.GetText(), p.caseSensitive)
		b, e1 := strconv.Atoi(before.GetText())
		a, e2 := strconv.Atoi(after.GetText())
		seconds, e3 := strconv.Atoi(cooldown.GetText())
		dir := strings.TrimSpace(directory.GetText())
		if err != nil || e1 != nil || e2 != nil || e3 != nil || b < 0 || b > 500 || a < 0 || a > 500 || seconds < 1 || seconds > 3600 || dir == "" || strings.TrimSpace(query.GetText()) == "" {
			w.message("留证参数", "填写有效过滤表达式、前后各 0–500 条、冷却 1–3600 秒及保存目录。")
			return
		}
		if p.collector == nil || p.dead {
			w.message("无法监听", "当前采集已停止。")
			return
		}
		w.closeModal()
		ctx, cancel := context.WithCancel(w.ctx)
		p.triggerCancel = cancel
		p.triggerStatus = "监听中"
		p.triggerID++
		id := p.triggerID
		c := p.collector
		done := make(chan struct{})
		w.triggerWorkers = append(w.triggerWorkers, done)
		go func() {
			defer close(done)
			defer cancel()
			err := core.WatchTrigger(ctx, c, core.TriggerOptions{Query: q, Before: b, After: a, Cooldown: time.Duration(seconds) * time.Second, Directory: dir, Redact: true}, func(status string) {
				w.post(func() {
					if !p.dead && p.triggerID == id && p.triggerCancel != nil {
						p.triggerStatus = status
						w.renderDirty = true
					}
				})
			})
			w.post(func() {
				if p.dead || p.triggerID != id {
					return
				}
				p.triggerCancel = nil
				cancel()
				if err != nil && err != context.Canceled {
					p.triggerStatus = "留证停止：" + err.Error()
				} else if err == nil {
					p.triggerStatus = "已保存 20 个问题包，监听自动停止"
				}
				w.renderDirty = true
			})
		}()
	}))
	if p.triggerStatus != "" {
		box.AddChild(w.diagnosticText(fmt.Sprintf("上次状态：%s", p.triggerStatus)))
	}
	w.show("关键词触发留证", box)
}
