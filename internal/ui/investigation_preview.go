package ui

import (
	"context"
	"os"
	"time"

	"loghelp/internal/core"
)

// Local UI profiling fixtures: no ADB commands, clipboard changes or exports.
func (w *app) profileInvestigationScene() {
	scene := os.Getenv("LOGHELP_PROFILE_SCENE")
	if scene != "json-inspector" && scene != "log-overview" && scene != "log-events" {
		return
	}
	p := w.active()
	if p == nil {
		p = w.newPage(core.TabState{Name: "诊断样本"})
	}
	ring := core.NewRing(100, 1<<20)
	for _, line := range []string{`10-19 12:00:00.000 100 100 I Demo: {"requestId":"中文-123","duration":1.235,"data":{"ok":true,"items":[1,2,3]}}`, `10-19 12:00:01.000 100 100 E AndroidRuntime: FATAL EXCEPTION: main`, `10-19 12:00:02.000 100 100 E ActivityManager: ANR in com.example.demo`} {
		ring.Add(core.ParseLine(line, time.Now()))
	}
	p.stopped, _, _ = ring.Snapshot()
	p.result.Rows = p.stopped
	p.rawResult = p.result
	p.selected = 0
	p.follow = false
	w.rebuild()
	switch scene {
	case "json-inspector":
		w.viewJSON()
	case "log-overview", "log-events":
		s := w.investigationSource(p)
		r, _ := core.AnalyzeLogs(context.Background(), s.scan)
		w.showInsights(s, r, scene == "log-events", 0)
	}
}
