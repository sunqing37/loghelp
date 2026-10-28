package ui

import (
	"context"
	"strings"
	"time"

	"loghelp/internal/core"
)

type foregroundFollow struct {
	page   *page
	client *core.ADB
	serial string
	cancel context.CancelFunc
	next   time.Time
	busy   bool
	status string
}

func (w *app) stopForegroundFollow() {
	if f := w.foregroundFollow; f != nil {
		if f.cancel != nil {
			f.cancel()
		}
		w.foregroundFollow = nil
	}
}

func (w *app) toggleForegroundFollow() {
	p := w.active()
	if p == nil || p.shell != nil || p.state.File != "" {
		return
	}
	w.syncInputs()
	p.state.FollowForeground = !p.state.FollowForeground
	w.stopForegroundFollow()
	w.save()
	w.rebuildDeviceLayout()
}

func (w *app) foregroundFollowStatus() string {
	p := w.active()
	if p == nil || !p.state.FollowForeground {
		return "关闭后保留当前包名"
	}
	if w.client == nil {
		return "等待配置 ADB"
	}
	if w.serialInput == nil || strings.TrimSpace(w.serialInput.GetText()) == "" {
		return "等待选择设备"
	}
	if strings.TrimSpace(w.serialInput.GetText()) != p.state.Serial {
		return "设备已修改，请开始采集"
	}
	if f := w.foregroundFollow; f != nil && f.page == p && f.status != "" {
		return shortText(f.status, 120)
	}
	return "正在读取顶部应用…"
}

func (w *app) updateForegroundFollow() {
	p := w.active()
	serial := ""
	if w.serialInput != nil {
		serial = strings.TrimSpace(w.serialInput.GetText())
	}
	if p == nil || p.dead || p.shell != nil || p.state.File != "" || !p.state.FollowForeground || w.client == nil || serial == "" || p.state.Serial != serial {
		w.stopForegroundFollow()
		return
	}
	f := w.foregroundFollow
	if f == nil || f.page != p || f.client != w.client || f.serial != serial {
		w.stopForegroundFollow()
		f = &foregroundFollow{page: p, client: w.client, serial: serial}
		w.foregroundFollow = f
	}
	if f.busy || time.Now().Before(f.next) || w.modal || w.fileDialogOpen {
		return
	}
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	f.cancel, f.busy = cancel, true
	go func() {
		pkg, err := f.client.ForegroundPackage(ctx, f.serial)
		cancel()
		w.post(func() {
			// A canceled request must never change a newly selected device/tab,
			// or overwrite the package after the switch has been turned off.
			if w.foregroundFollow != f {
				return
			}
			f.busy, f.cancel, f.next = false, nil, time.Now().Add(time.Second)
			if w.active() != p || p.dead || !p.state.FollowForeground || p.state.File != "" || p.shell != nil || w.client != f.client || p.state.Serial != f.serial || w.serialInput == nil || strings.TrimSpace(w.serialInput.GetText()) != f.serial {
				w.stopForegroundFollow()
				return
			}
			if err != nil {
				f.status = err.Error()
				return
			}
			f.status = "正在跟随设备顶部应用"
			if w.modal || w.fileDialogOpen || w.appInput == nil {
				return
			}
			if p.state.Package == pkg && strings.TrimSpace(w.appInput.GetText()) == pkg {
				return
			}
			// Update only the package restriction. Draft filter/search input is
			// still applied through the existing input debounce.
			p.state.Package = pkg
			w.appInput.SetText(pkg)
			p.offset = 0
			w.refresh(p, true)
		})
	}()
}
