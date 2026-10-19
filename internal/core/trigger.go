package core

import (
	"context"
	"fmt"
	"time"
)

type TriggerOptions struct {
	Query         Query
	Before, After int
	Cooldown      time.Duration
	Directory     string
	Redact        bool
}

// WatchTrigger is independent of the view's pause/filter. It reads the bounded
// ring and records losses, rather than claiming to capture overwritten entries.
func WatchTrigger(ctx context.Context, c *Collector, opt TriggerOptions, notify func(string)) error {
	if opt.Before < 0 || opt.Before > 500 || opt.After < 0 || opt.After > 500 || opt.Cooldown < time.Second || opt.Query.Match == nil {
		return fmt.Errorf("留证参数无效")
	}
	last := c.Ring.Version()
	start := uint64(0)
	if last > uint64(opt.Before) {
		start = last - uint64(opt.Before)
	}
	pre, initialVersion, initialLost := c.Ring.Since(start, max(1, opt.Before))
	last = initialVersion
	if opt.Before == 0 {
		pre = nil
		initialLost = 0
	}
	var pending []*Entry
	var pendingBytes int64
	var hit, deadline, next time.Time
	var hitSeq, lost uint64
	lost = initialLost
	saved := 0
	trim := func() {
		for len(pre) > opt.Before {
			pre[0] = nil
			pre = pre[1:]
		}
		var bytes int64
		for _, e := range pre {
			bytes += e.Size()
		}
		for bytes > 4<<20 && len(pre) > 0 {
			bytes -= pre[0].Size()
			pre[0] = nil
			pre = pre[1:]
			lost++
		}
	}
	trim()
	flush := func() error {
		if pending == nil {
			return nil
		}
		note := fmt.Sprintf("后文最多等待 10 秒；实际保存 %d 条，可能因采集结束或等待超时不足。触发序号、时间和读取缺口见 trigger 字段。", len(pending))
		scan := func(ctx context.Context, visit func(*Entry) bool) error {
			for _, e := range pending {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !visit(e) {
					break
				}
			}
			return nil
		}
		r, err := BuildEvidence(ctx, scan, EvidenceOptions{Note: note, Serial: c.Serial, Scope: "关键词触发 · 未过滤缓存", Redact: opt.Redact, Trigger: &TriggerDetails{Sequence: hitSeq, DetectedAt: hit, Before: opt.Before, After: opt.After, Lost: lost}})
		if err != nil {
			return err
		}
		path := EvidencePath(opt.Directory)
		if err = r.Save(ctx, path); err != nil {
			return err
		}
		saved++
		notify(fmt.Sprintf("已留证 %d / 20：%s", saved, path))
		pending = nil
		next = time.Now().Add(opt.Cooldown)
		return nil
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		ended := false
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			ended = true
		case <-ticker.C:
		}
		rows, version, gap := c.Ring.Since(last, 5000)
		last = version
		lost += gap
		for _, e := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if pending != nil {
				pending = append(pending, e)
				pendingBytes += e.Size()
				if pendingBytes > 8<<20 {
					return fmt.Errorf("触发上下文超过 8 MiB，监听已停止，请减少前后条数")
				}
				if int(e.Seq-hitSeq) >= opt.After {
					if err := flush(); err != nil {
						return err
					}
					if saved >= 20 {
						return nil
					}
				}
			}
			if pending == nil && !e.Event && !time.Now().Before(next) && opt.Query.Match(e, time.Now()) {
				pending = append(append([]*Entry(nil), pre...), e)
				pendingBytes = 0
				for _, v := range pending {
					pendingBytes += v.Size()
				}
				if pendingBytes > 8<<20 {
					return fmt.Errorf("触发上下文超过 8 MiB，监听已停止，请减少前后条数")
				}
				hit = time.Now()
				hitSeq = e.Seq
				deadline = hit.Add(10 * time.Second)
				if opt.After == 0 {
					if err := flush(); err != nil {
						return err
					}
					if saved >= 20 {
						return nil
					}
				} else {
					notify("已命中，正在等待后文…")
				}
			}
			pre = append(pre, e)
			trim()
		}
		if pending != nil && (ended || time.Now().After(deadline)) {
			if err := flush(); err != nil {
				return err
			}
			if saved >= 20 {
				return nil
			}
		}
		if ended {
			return fmt.Errorf("采集已结束，触发监听已停止")
		}
	}
}
