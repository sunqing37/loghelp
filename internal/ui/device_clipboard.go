package ui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"loghelp/internal/core"
)

// Serialize OS clipboard access across preview generations. A replacement
// worker waits for the cancelled worker's bounded OS operation to finish.
var deviceClipboardMu sync.Mutex

func (w *app) stopDeviceClipboard(d *devicePreview) {
	if d.clipboardCancel != nil {
		d.clipboardCancel()
		d.clipboardCancel = nil
	}
	d.clipboardStream = nil
}

func (w *app) updateDeviceClipboard(d *devicePreview) {
	if !d.clipboardEnabled || !d.ready || d.failed || d.ctx.Err() != nil {
		w.stopDeviceClipboard(d)
		return
	}
	if d.clipboardStream == d.stream {
		return
	}
	w.stopDeviceClipboard(d)
	stream := d.stream
	ctx, cancel := context.WithCancel(d.ctx)
	d.clipboardCancel, d.clipboardStream = cancel, stream
	d.clipboardStatus = "文本剪贴板共享中"
	done := make(chan struct{})
	w.triggerWorkers = append(w.triggerWorkers, done)
	go func() {
		defer close(done)
		deviceClipboardMu.Lock()
		defer deviceClipboardMu.Unlock()
		lastReport := ""
		report := func(status string) {
			if status == lastReport || ctx.Err() != nil {
				return
			}
			lastReport = status
			w.post(func() {
				if ctx.Err() == nil && w.preview == d && d.stream == stream && d.clipboardStatus != status {
					d.clipboardStatus = status
					w.updateDeviceChrome()
				}
			})
		}
		if err := shareDeviceClipboard(ctx, stream, report); err != nil && ctx.Err() == nil {
			report("剪贴板共享失败：" + err.Error() + "；可关闭再开启共享，连接异常请重连预览")
		}
	}()
}

func shareDeviceClipboard(ctx context.Context, stream *core.ScreenStream, report func(string)) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Enabling/reconnecting starts from the current clipboard, not old events.
	for {
		select {
		case <-stream.ClipboardChanges():
			continue
		default:
		}
		break
	}
	last, hadText, err := readSharedClipboard(ctx)
	initialized := err == nil
	if err != nil {
		report("读取电脑剪贴板失败：" + err.Error())
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		var remote string
		var received bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stream.Done():
			return nil
		case remote = <-stream.ClipboardChanges():
			received = true
		case <-ticker.C:
		}
		if err := stream.ClipboardError(); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		local, hasText, err := readSharedClipboard(ctx)
		if err != nil {
			report("读取电脑剪贴板失败：" + err.Error())
			if !received {
				continue
			}
		} else if !initialized {
			last, hadText, initialized = local, hasText, true
		}
		// If both sides changed in this sampling interval, keep the newly
		// copied computer text. This also prevents delayed echoes overwriting it.
		if hasText && (!hadText || local != last) {
			last, hadText = local, true
			if err := core.ValidateScreenClipboardText(local); err != nil {
				report(err.Error())
				continue
			}
			request, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := stream.SetClipboardText(request, local)
			cancel()
			if err != nil {
				return err
			}
			report("文本剪贴板共享中")
			continue
		}
		last, hadText = local, hasText
		if !received || (hasText && remote == local) {
			continue
		}
		if err := core.ValidateScreenClipboardText(remote); err != nil {
			report(err.Error())
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := writeSharedClipboard(ctx, remote); err != nil {
			return fmt.Errorf("写入电脑剪贴板：%w", err)
		}
		// Only mark the text we wrote as synchronized. A read-back could
		// swallow a new user copy made immediately after our OS write.
		last, hadText = remote, true
		initialized = true
		report("文本剪贴板共享中")
	}
}
