package ui

import (
	"context"
	"image/color"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	qrcode "github.com/skip2/go-qrcode"
	"loghelp/internal/core"
)

func (w *app) wirelessQR() {
	client := w.client
	if client == nil {
		w.message("Wi-Fi 扫码配对", "请先在设置中配置可用的 ADB。")
		return
	}
	session, err := core.NewWirelessQRPairing()
	if err != nil {
		w.message("Wi-Fi 扫码配对", err.Error())
		return
	}
	qr, err := qrcode.New(session.Payload(), qrcode.Medium)
	if err != nil {
		w.message("Wi-Fi 扫码配对", "生成二维码失败，请重新打开扫码配对。")
		return
	}
	// Keep a white quiet zone and integer pixels per module at every UI scale.
	modules := len(qr.Bitmap())
	size := modules * max(3, w.px(240)/modules)
	texture := ebiten.NewImageFromImage(qr.Image(size))
	graphic := widget.NewGraphic(widget.GraphicOpts.Image(texture))
	contentWidth := w.wirelessContentWidth()
	instruction := w.label(w.wirelessText("手机打开「开发者选项 → 无线调试 → 使用二维码配对」，扫描下方二维码。手机与电脑需在同一可互访网络。", contentWidth))
	status := w.label(w.wirelessText("等待手机扫码…（2 分钟内有效，关闭窗口可取消）", contentWidth))
	status.GetWidget().MinHeight = w.px(70)
	box := w.column(instruction, graphic, status,
		w.row(w.button("刷新二维码", w.wirelessQR), w.button("配对码配对", func() { w.wirelessForm("pair", "") })),
		w.action("地址直连", "quiet", func() { w.wirelessForm("connect", "") }),
	)
	box.GetWidget().MinWidth = contentWidth
	w.show("Wi-Fi 扫码配对", box)
	owner := w.modalWindow
	setStatus := func(value string) {
		status.Label = w.wirelessText(value, contentWidth)
		box.RequestRelayout()
		owner.RequestRelayout()
		w.renderDirty = true
	}
	ctx, cancel := context.WithTimeout(w.ctx, 2*time.Minute)
	w.wirelessCancel = cancel
	w.wirelessCleanup = func() { texture.Deallocate() }
	go func() {
		defer cancel()
		err := client.PairWirelessQR(ctx, session, func(message string) {
			w.post(func() {
				if !w.closing.Load() && w.modalWindow == owner && w.client == client {
					setStatus(message)
				}
			})
		})
		// Use the session context so closing the dialog also cancels refresh.
		refreshCtx, refreshCancel := context.WithTimeout(ctx, 5*time.Second)
		devices, refreshErr := client.Devices(refreshCtx)
		refreshCancel()
		w.post(func() {
			if w.closing.Load() || w.modalWindow != owner || w.client != client {
				return
			}
			w.wirelessCancel = nil
			// Invalidate the displayed credentials after success, failure or timeout.
			texture.Fill(color.White)
			if refreshErr == nil {
				w.devices = devices
			}
			if err != nil {
				setStatus(err.Error())
			} else {
				setStatus("扫码配对成功。关闭后从「选择设备」选择在线 Wi-Fi 设备；ADB 会自动尝试连接，若未出现设备可使用「地址直连」。")
			}
		})
	}()
}
