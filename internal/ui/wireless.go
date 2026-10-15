package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
)

func (w *app) cancelWirelessOperation() {
	if w.wirelessCancel != nil {
		w.wirelessCancel()
		w.wirelessCancel = nil
	}
	if w.wirelessCleanup != nil {
		w.wirelessCleanup()
		w.wirelessCleanup = nil
	}
}

func (w *app) wirelessDevices() {
	if w.client == nil {
		w.message("Wi-Fi 设备", "请先在设置中配置可用的 ADB。")
		return
	}
	w.syncInputs()
	w.show("Wi-Fi 设备", w.column(
		w.label("手机与电脑需连接同一可互访的 Wi-Fi 网络。"),
		w.button("地址直连 / 重新连接", func() { w.wirelessForm("connect", "") }),
		w.muted("已配对或已开启 TCP 调试的设备，输入 IP:连接端口。"),
		w.button("Android 11+ 扫码配对", w.wirelessQR),
		w.muted("手机：无线调试 → 使用二维码配对，扫描电脑上的二维码。"),
		w.button("Android 11+ 配对码配对", func() { w.wirelessForm("pair", "") }),
		w.muted("手机：开发者选项 → 无线调试 → 使用配对码配对。"),
		w.button("USB 切换 Wi-Fi", w.chooseWirelessUSB),
		w.muted("先连接并授权 USB 真机，再开启 TCP 调试并连接。"),
		w.button("断开指定无线设备", w.chooseWirelessDisconnect),
		w.muted("断开会中断该设备的采集、录制、终端及投屏。"),
	))
}

func (w *app) chooseWirelessUSB() {
	var labels []string
	serials := map[string]string{}
	for _, d := range w.devices {
		if d.State == "device" && d.Connection == "USB" {
			label := d.Name + " · " + d.Serial
			labels = append(labels, label)
			serials[label] = d.Serial
		}
	}
	if len(labels) == 0 {
		w.message("USB 切换 Wi-Fi", "未发现在线 USB 真机，请连接数据线并在手机上允许 USB 调试，等待设备列表刷新后重试。")
		return
	}
	w.choices("选择要切换的 USB 真机", labels, func(label string) { w.wirelessForm("usb", serials[label]) })
}

func (w *app) chooseWirelessDisconnect() {
	var labels []string
	serials := map[string]string{}
	for _, d := range w.devices {
		if d.Connection == "Wi-Fi" {
			label := fmt.Sprintf("%s · %s · %s", d.Name, d.Serial, d.State)
			labels = append(labels, label)
			serials[label] = d.Serial
		}
	}
	if len(labels) == 0 {
		w.message("断开无线设备", "当前没有无线设备连接。")
		return
	}
	w.choices("选择要断开的无线设备", labels, func(label string) { w.wirelessForm("disconnect", serials[label]) })
}

func (w *app) wirelessForm(mode, serial string) {
	client := w.client
	if client == nil {
		w.message("Wi-Fi 设备", "请先在设置中配置可用的 ADB。")
		return
	}
	title, action := "Wi-Fi 地址直连", "连接"
	hint := "输入无线调试主页的 IP 地址和连接端口，或 TCP 调试地址。"
	success := "连接成功。关闭此窗口后，在「选择设备」中选择 Wi-Fi 设备并开始采集。"
	switch mode {
	case "pair":
		title, action = "Wi-Fi 配对", "配对"
		hint = "保持手机配对码页面打开，输入该页面的 IP:配对端口和 6 位配对码。"
		success = "配对成功。请返回无线调试主页，使用「地址直连」输入 IP:连接端口；两个端口通常不同。"
	case "usb":
		title, action = "USB 切换 Wi-Fi", "开启并连接"
		hint = "输入此手机的 Wi-Fi IP:端口（通常为 5555）。开启 TCP 调试会重启 adbd，中断该设备现有会话。仅在可信网络使用。"
	case "disconnect":
		title, action = "断开无线设备", "确认断开"
		hint = "会中断该设备的采集、录制、终端及投屏；仅断开此连接。此操作不会关闭手机的无线/TCP 调试开关。"
		success = "已断开指定无线设备。"
	}
	box := w.column()
	description := w.label(hint)
	description.MaxWidth = float64(w.px(640))
	box.AddChild(description)
	if serial != "" {
		device := w.label("目标设备：" + serial)
		device.MaxWidth = float64(w.px(640))
		box.AddChild(device)
	}
	address := w.input("", "例如 192.168.1.10:5555", nil, nil)
	code := w.input("", "手机显示的 6 位配对码", nil, nil)
	if mode != "disconnect" {
		box.AddChild(w.muted("设备地址（IP:端口）"), address)
	}
	if mode == "pair" {
		box.AddChild(w.muted("配对码（不保存）"), code)
	}
	status := w.label("等待操作。连接失败时请检查地址、调试授权和 Wi-Fi 网络。")
	status.MaxWidth = float64(w.px(640))
	status.GetWidget().MinHeight = w.px(80)
	var owner *widget.Window
	var submit *widget.Button
	busy := false
	submit = w.action(action, "primary", func() {
		if busy {
			return
		}
		endpoint, pairingCode := strings.TrimSpace(address.GetText()), strings.TrimSpace(code.GetText())
		if mode != "disconnect" {
			var err error
			endpoint, err = core.NormalizeADBAddress(endpoint)
			if err != nil {
				status.Label = err.Error()
				return
			}
		}
		busy = true
		submit.GetWidget().Disabled = true
		address.GetWidget().Disabled = true
		code.GetWidget().Disabled = true
		status.Label = "正在" + action + "，请稍候…（最长 30 秒，关闭窗口可取消）"
		ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
		w.wirelessCancel = cancel
		go func() {
			defer cancel()
			var err error
			switch mode {
			case "pair":
				err = client.PairWireless(ctx, endpoint, pairingCode)
			case "usb":
				err = client.EnableWireless(ctx, serial, endpoint)
			case "disconnect":
				err = client.DisconnectWireless(ctx, serial)
			default:
				err = client.ConnectWireless(ctx, endpoint)
			}
			// Refresh even on failure: USB mode may have changed before connect failed.
			refreshCtx, refreshCancel := context.WithTimeout(ctx, 5*time.Second)
			devices, refreshErr := client.Devices(refreshCtx)
			refreshCancel()
			w.post(func() {
				if w.closing.Load() || w.modalWindow != owner || w.client != client {
					return
				}
				w.wirelessCancel = nil
				busy = false
				submit.GetWidget().Disabled = false
				address.GetWidget().Disabled = false
				code.GetWidget().Disabled = false
				code.SetText("")
				if refreshErr == nil {
					w.devices = devices
				}
				if err != nil {
					status.Label = err.Error()
				} else {
					status.Label = success
				}
				if refreshErr != nil {
					status.Label += "\n设备列表暂未刷新，请关闭窗口后等待自动刷新。"
				}
				box.RequestRelayout()
			})
		}()
	})
	box.AddChild(status, submit)
	if mode == "pair" {
		box.AddChild(w.button("下一步：地址直连", func() { w.wirelessForm("connect", "") }))
	}
	box.AddChild(w.action("返回 Wi-Fi 设备", "quiet", w.wirelessDevices))
	w.show(title, box)
	owner = w.modalWindow
	if mode != "disconnect" {
		w.ui.SetFocusedWidget(address)
	}
}
