package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
)

type refreshChoice struct {
	label  string
	choose func()
}

func (w *app) cancelListRefresh() {
	if w.listCancel != nil {
		w.listCancel()
		w.listCancel = nil
	}
}

// Keep the same dialog/search control while replacing only its result rows.
// Refresh stays in the fixed footer, including when the list is empty.
func (w *app) refreshableChoices(title, empty string, load func(context.Context) ([]refreshChoice, error)) {
	width := w.wirelessContentWidth()
	search := w.input("", "筛选列表（刷新后保留）", nil, nil)
	status := w.label("准备刷新…")
	list := w.column()
	box := w.column(search, status, list)
	box.GetWidget().MinWidth, box.GetWidget().MinHeight = width, w.px(320)
	var owner *widget.Window
	var values []refreshChoice
	busy := false
	render := func() {
		list.RemoveChildren()
		filter := strings.ToLower(search.GetText())
		count := 0
		for _, choice := range values {
			if !strings.Contains(strings.ToLower(choice.label), filter) {
				continue
			}
			choice := choice
			button := w.button(w.wirelessText(choice.label, width-w.px(28)), func() {
				if busy {
					return
				}
				w.closeModal()
				choice.choose()
			})
			button.GetWidget().Disabled = busy
			list.AddChild(button)
			count++
		}
		if count == 0 {
			message := empty
			if filter != "" && len(values) > 0 {
				message = "没有匹配项，可修改筛选文字或刷新。"
			}
			if busy {
				message = "正在读取列表…"
			}
			list.AddChild(w.label(w.wirelessText(message, width)))
		}
		box.RequestRelayout()
		if owner != nil {
			owner.RequestRelayout()
		}
		w.renderDirty = true
	}
	var refresh *widget.Button
	reload := func() {
		if busy {
			return
		}
		busy = true
		refresh.GetWidget().Disabled = true
		refresh.SetText("刷新中…")
		status.Label = "正在刷新，关闭窗口可取消。"
		render()
		ctx, cancel := context.WithTimeout(w.ctx, 15*time.Second)
		w.listCancel = cancel
		go func() {
			choices, err := load(ctx)
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			w.post(func() {
				defer cancel()
				if w.closing.Load() || w.modalWindow != owner || ctx.Err() == context.Canceled {
					return
				}
				w.listCancel = nil
				busy = false
				refresh.GetWidget().Disabled = false
				refresh.SetText("刷新")
				if err != nil {
					message := "刷新失败，保留上次列表，可重试：" + err.Error()
					if err == context.DeadlineExceeded {
						message = "刷新超时，保留上次列表。请检查设备与 ADB 后重试。"
					}
					status.Label = w.wirelessText(message, width)
				} else {
					values = choices
					status.Label = fmt.Sprintf("已刷新 %s · %d 项", time.Now().Format("15:04:05"), len(values))
				}
				render()
			})
		}()
	}
	refresh = w.action("刷新", "primary", reload)
	search.ChangedEvent.AddHandler(func(any) { render() })
	render()
	w.showDialog(title, box, refresh)
	owner = w.modalWindow
	reload()
}

func (w *app) deviceChoices(title, empty string, include func(core.Device) bool, choose func(core.Device)) {
	client := w.client
	if client == nil {
		w.message(title, "请先在设置中配置可用的 ADB。")
		return
	}
	w.refreshableChoices(title, empty, func(ctx context.Context) ([]refreshChoice, error) {
		devices, err := client.Devices(ctx)
		if err != nil {
			return nil, err
		}
		var result []refreshChoice
		for _, device := range devices {
			if include != nil && !include(device) {
				continue
			}
			device := device
			result = append(result, refreshChoice{
				label: fmt.Sprintf("%s · %s · %s · %s", device.Name, device.Serial, device.State, device.Connection),
				choose: func() {
					if w.client == client {
						choose(device)
					}
				},
			})
		}
		return result, nil
	})
}
