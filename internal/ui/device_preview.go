package ui

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ncruces/zenity"
	"loghelp/internal/core"
)

type devicePreview struct {
	client                                       *core.ADB
	serial                                       string
	ctx                                          context.Context
	cancel                                       context.CancelFunc
	stream                                       *core.ScreenStream
	recording                                    *core.ScreenRecording
	record                                       *widget.Button
	recordSettings                               *widget.Button
	recordPreparing                              bool
	recordPreparationCancel                      context.CancelFunc
	recordLabel                                  *widget.Text
	recordStatus                                 string
	texture                                      *ebiten.Image
	shader                                       *ebiten.Shader
	frameSize                                    image.Point
	fullRange, bt709, rgb                        bool
	paused, busy, saving, ready, failed          bool
	status, notice                               string
	statsAt                                      time.Time
	statsFrames                                  uint64
	panel                                        *widget.Container
	statusLabel                                  *widget.Text
	pause, reconnect, screenshot, copyScreenshot *widget.Button
	keys                                         []*widget.Button
	dragging                                     bool
	dragSize, lastTouch                          image.Point
	dragRect                                     image.Rectangle
}

const deviceVideoShader = `//kage:unit pixels
package main
var FullRange float
var BT709 float
var RGB float
func Fragment(position vec4, texCoord vec2, color vec4) vec4 {
    p := imageSrc0At(texCoord)
	if RGB > 0.5 { return p }
    y := p.r
    u := p.g - 128.0/255.0
    v := p.b - 128.0/255.0
    if FullRange < 0.5 {
        y = (y - 16.0/255.0) * (255.0/219.0)
        u *= 255.0/224.0
        v *= 255.0/224.0
    }
    rgb := vec3(y + 1.402*v, y - 0.344136*u - 0.714136*v, y + 1.772*u)
    if BT709 > 0.5 {
        rgb = vec3(y + 1.5748*v, y - 0.187324*u - 0.468124*v, y + 1.8556*u)
    }
    return vec4(clamp(rgb, vec3(0), vec3(1)), 1)
}`

func (w *app) openDevicePreview() {
	if w.preview != nil {
		w.closeDevicePreview()
		return
	}
	if w.client == nil {
		w.settings()
		return
	}
	serial := ""
	if p := w.active(); p != nil {
		serial = p.state.Serial
		if p.shell == nil && p.state.File == "" && w.serialInput != nil {
			serial = strings.TrimSpace(w.serialInput.GetText())
		}
	}
	if serial == "" {
		w.choosePreviewDevice()
		return
	}
	w.startDevicePreview(serial)
}

func (w *app) choosePreviewDevice() {
	if w.client == nil {
		w.settings()
		return
	}
	w.deviceChoices("选择预览设备", "没有在线设备。检查 USB / Wi-Fi 连接及授权后点击刷新。",
		func(d core.Device) bool { return d.State == "device" },
		func(d core.Device) { w.startDevicePreview(d.Serial) })
}

func (w *app) cancelDeviceTouch() {
	d := w.preview
	if d != nil && d.dragging {
		d.dragging = false
		if d.stream != nil {
			if err := d.stream.Touch(3, d.lastTouch, d.dragSize); err != nil {
				d.notice = err.Error()
			}
		}
	}
}

func (w *app) disposeDevicePreview() {
	if d := w.preview; d != nil {
		w.cancelDeviceTouch()
		w.cancelPreparingDeviceRecording(d)
		d.cancel()
		if d.stream != nil {
			d.stream.Close()
		}
		if d.texture != nil {
			d.texture.Dispose()
		}
		if d.shader != nil {
			d.shader.Dispose()
		}
		w.preview = nil
	}
}

func (w *app) closeDevicePreview() {
	w.disposeDevicePreview()
	ebiten.SetWindowSizeLimits(1000, 720, -1, -1)
	w.rebuildDeviceLayout()
}

func (w *app) startDevicePreview(serial string) {
	shader, err := ebiten.NewShader([]byte(deviceVideoShader))
	if err != nil {
		w.message("投屏渲染失败", err.Error())
		return
	}
	w.disposeDevicePreview()
	ctx, cancel := context.WithCancel(w.ctx)
	w.preview = &devicePreview{client: w.client, serial: serial, ctx: ctx, cancel: cancel, shader: shader, status: "正在连接视频投屏…", statsAt: time.Now()}
	w.preview.stream = core.StartScreenStream(ctx, w.client, serial)
	w.previewStreams = append(w.previewStreams, w.preview.stream)
	ebiten.SetWindowSizeLimits(1440, 720, -1, -1)
	width, height := ebiten.WindowSize()
	if width < 1440 {
		ebiten.SetWindowSize(1440, height)
	}
	w.rebuildDeviceLayout()
}

func (w *app) reconnectDevicePreview() {
	d := w.preview
	if d == nil {
		return
	}
	w.cancelDeviceTouch()
	w.cancelPreparingDeviceRecording(d)
	w.replaceDevicePreviewStream(d, core.ScreenRecordingOptions{})
	d.paused = false
	d.status, d.notice = "正在重新连接视频投屏…", ""
	w.updateDeviceChrome()
}

func (w *app) devicePanel() *widget.Container {
	d := w.preview
	w.cancelDeviceTouch()
	header := w.stretchRow(w.label("设备投屏"), w.row(w.action("切换", "quiet", w.choosePreviewDevice), w.action("×", "quiet", w.closeDevicePreview)))
	serial := w.muted(d.serial)
	serial.MaxWidth = float64(w.px(288))
	d.pause = w.action("暂停", "quiet", func() { w.cancelDeviceTouch(); d.paused = !d.paused; w.updateDeviceChrome() })
	d.reconnect = w.action("重连", "quiet", w.reconnectDevicePreview)
	d.screenshot = w.action("保存截图", "primary", func() { w.captureDeviceScreenshot(false) })
	d.copyScreenshot = w.action("复制截图", "quiet", func() { w.captureDeviceScreenshot(true) })
	d.record = w.action("开始录屏", "primary", w.toggleDeviceRecording)
	d.recordSettings = w.action("录屏设置", "quiet", w.deviceRecordingSettings)
	d.recordLabel = w.muted("")
	d.recordLabel.MaxWidth = float64(w.px(288))
	d.panel = widget.NewContainer(widget.ContainerOpts.WidgetOpts(widget.WidgetOpts.MinSize(w.px(160), w.px(180))))
	d.keys = nil
	buttons := w.row()
	for _, item := range []struct {
		label string
		code  uint32
	}{{"返回", 4}, {"主页", 3}, {"最近", 187}, {"唤醒", 224}} {
		code := item.code
		button := w.action(item.label, "quiet", func() { w.sendDeviceControl(func(s *core.ScreenStream) error { return s.Key(code) }) })
		d.keys = append(d.keys, button)
		buttons.AddChild(button)
	}
	d.statusLabel = w.muted(d.status)
	d.statusLabel.MaxWidth = float64(w.px(288))
	d.statusLabel.GetWidget().MinHeight = w.px(64)
	content := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{false, false, false, false, false, false, true, false, false, false}), widget.GridLayoutOpts.Spacing(0, w.px(10)))))
	content.AddChild(header, serial, w.row(d.pause, d.reconnect), w.row(d.screenshot, d.copyScreenshot), w.row(d.record, d.recordSettings), d.recordLabel, d.panel, buttons, w.muted("视频投屏 · 点击／拖动操作设备"), d.statusLabel)
	box := w.inset(content, 12, w.colors().sidebar)
	box.GetWidget().MinWidth = w.px(320)
	box.GetWidget().LayoutData = widget.GridLayoutData{MaxWidth: w.px(320)}
	w.updateDeviceChrome()
	return box
}

func (w *app) updateDeviceChrome() {
	d := w.preview
	if d == nil || d.statusLabel == nil {
		return
	}
	status := d.status
	if d.paused {
		status = "显示已暂停 · " + status
	}
	if d.notice != "" {
		status += "\n" + d.notice
	}
	runes := []rune(status)
	if len(runes) > 160 {
		runes = append(runes[:160], '…')
	}
	d.statusLabel.Label = string(runes)
	d.pause.SetText("暂停")
	if d.paused {
		d.pause.SetText("继续")
	}
	d.screenshot.GetWidget().Disabled = d.busy || d.saving
	d.copyScreenshot.GetWidget().Disabled = d.busy || d.saving
	w.updateDeviceRecordingChrome(d)
	for _, button := range d.keys {
		button.GetWidget().Disabled = !d.ready || d.paused || d.failed || d.busy
	}
	w.ui.Container.RequestRelayout()
	w.renderDirty = true
}

func (w *app) captureDeviceScreenshot(copyImage bool) {
	d := w.preview
	if d == nil || d.busy || d.saving {
		return
	}
	w.cancelDeviceTouch()
	d.busy = true
	d.notice = "正在获取原分辨率截图…"
	w.updateDeviceChrome()
	go func() {
		frame, err := d.client.CaptureScreen(d.ctx, d.serial)
		if err == nil && copyImage && d.ctx.Err() == nil {
			err = copyScreenshotToClipboard(frame)
		}
		w.post(func() {
			if w.preview != d || d.ctx.Err() != nil {
				return
			}
			d.busy = false
			if err != nil {
				d.notice = err.Error()
			} else if copyImage {
				d.notice = fmt.Sprintf("截图已复制 · %d × %d", frame.Image.Bounds().Dx(), frame.Image.Bounds().Dy())
			} else {
				d.notice = "截图已获取，请选择保存位置"
				w.saveDeviceScreenshot(d, frame)
			}
			w.updateDeviceChrome()
		})
	}()
}

func (w *app) saveDeviceScreenshot(d *devicePreview, frame *core.ScreenFrame) {
	name := "screenshot-" + frame.CapturedAt.Format("20060102-150405.000") + ".png"
	w.fileDialog("save", "保存设备截图（PNG 原分辨率）", filepath.Join(w.dialogDirectory(""), name), zenity.FileFilters{{Name: "PNG 截图", Patterns: []string{"*.png"}, CaseFold: true}}, func(path string) {
		if !strings.EqualFold(filepath.Ext(path), ".png") {
			w.message("保存截图", "截图请保存为 .png 文件。")
			return
		}
		d.saving = true
		w.updateDeviceChrome()
		go func() {
			err := writeDeviceScreenshot(path, frame.PNG)
			w.post(func() {
				d.saving = false
				if w.preview != d {
					return
				}
				if err != nil {
					d.notice = err.Error()
					w.message("截图保存失败", err.Error())
				} else {
					d.notice = "已保存：" + filepath.Base(path)
				}
				w.updateDeviceChrome()
			})
		}()
	})
}

func writeDeviceScreenshot(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".loghelp-screenshot-*.png")
	if err != nil {
		return fmt.Errorf("创建截图文件 %s：%w", path, err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary, path)
	}
	if err != nil {
		return fmt.Errorf("保存截图 %s：%w", path, err)
	}
	return nil
}

func (w *app) sendDeviceControl(work func(*core.ScreenStream) error) bool {
	d := w.preview
	if d == nil || !d.ready || d.paused || d.failed || d.busy {
		return false
	}
	if err := work(d.stream); err != nil {
		d.notice = err.Error()
		w.updateDeviceChrome()
		return false
	}
	return true
}

func (w *app) deviceScreenRect() image.Rectangle {
	d := w.preview
	if d == nil || d.panel == nil || d.frameSize.X < 1 || d.frameSize.Y < 1 {
		return image.Rectangle{}
	}
	area, size := d.panel.GetWidget().Rect, d.frameSize
	if area.Empty() {
		return image.Rectangle{}
	}
	scale := math.Min(float64(area.Dx())/float64(size.X), float64(area.Dy())/float64(size.Y))
	width, height := max(1, int(float64(size.X)*scale)), max(1, int(float64(size.Y)*scale))
	x, y := area.Min.X+(area.Dx()-width)/2, area.Min.Y+(area.Dy()-height)/2
	return image.Rect(x, y, x+width, y+height)
}

// Draw video after the cached workspace: new frames never invalidate all logs.
func (w *app) drawDeviceScreen(screen *ebiten.Image) {
	d := w.preview
	if d == nil || d.panel == nil || w.modal {
		return
	}
	area := d.panel.GetWidget().Rect.Intersect(screen.Bounds())
	if area.Empty() {
		return
	}
	clip := screen.SubImage(area).(*ebiten.Image)
	clip.Fill(w.colors().bg)
	if d.texture == nil {
		w.paintFace(clip, "等待设备视频", float64(area.Min.X+w.px(12)), float64(area.Min.Y+w.px(20)), w.smallFace, w.colors().muted)
		return
	}
	rect := w.deviceScreenRect()
	fullRange, bt709, rgb := float32(0), float32(0), float32(0)
	if d.fullRange {
		fullRange = 1
	}
	if d.bt709 {
		bt709 = 1
	}
	if d.rgb {
		rgb = 1
	}
	opts := &ebiten.DrawRectShaderOptions{Images: [4]*ebiten.Image{d.texture}, Uniforms: map[string]any{"FullRange": fullRange, "BT709": bt709, "RGB": rgb}}
	opts.GeoM.Scale(float64(rect.Dx())/float64(d.frameSize.X), float64(rect.Dy())/float64(d.frameSize.Y))
	opts.GeoM.Translate(float64(rect.Min.X), float64(rect.Min.Y))
	clip.DrawRectShader(d.frameSize.X, d.frameSize.Y, d.shader, opts)
}

func (w *app) updateDevicePreview(allowInput bool) {
	d := w.preview
	if d == nil {
		return
	}
	w.pollDeviceRecording(d)
	if d.client != w.client {
		w.closeDevicePreview()
		return
	}
	ready, frames, err := d.stream.State()
	if ready != d.ready {
		d.ready = ready
		w.updateDeviceChrome()
	}
	if err != nil && !d.failed {
		w.cancelDeviceTouch()
		d.failed = true
		d.status = err.Error() + "；点击重连重试"
		if d.texture != nil {
			d.texture.Dispose()
			d.texture = nil
		}
		d.frameSize = image.Point{}
		w.updateDeviceChrome()
	}
	if frame := d.stream.TakeFrame(); frame != nil {
		if !d.paused && !d.failed {
			if d.frameSize != frame.Size {
				w.cancelDeviceTouch()
				if d.texture != nil {
					d.texture.Dispose()
				}
				d.texture = ebiten.NewImage(frame.Size.X, frame.Size.Y)
				d.status = fmt.Sprintf("%d × %d · 视频已连接", frame.Size.X, frame.Size.Y)
				w.updateDeviceChrome()
			}
			d.frameSize, d.fullRange, d.bt709 = frame.Size, frame.FullRange, frame.BT709
			d.rgb = frame.RGB
			d.texture.WritePixels(frame.Pixels)
		}
		d.stream.ReleaseFrame(frame)
	}
	if ready && !d.failed && time.Since(d.statsAt) >= time.Second {
		fps := float64(frames-d.statsFrames) / time.Since(d.statsAt).Seconds()
		d.status = fmt.Sprintf("%d × %d · %.0f FPS", d.frameSize.X, d.frameSize.Y, fps)
		d.statsAt, d.statsFrames = time.Now(), frames
		w.updateDeviceChrome()
	}
	if w.backgroundDiagnostic && os.Getenv("LOGHELP_PROFILE_SCENE") == "device-preview" && os.Getenv("LOGHELP_PROFILE_COPY_SCREENSHOT") == "1" && !w.profileDeviceCopyDone && ready && d.texture != nil {
		w.profileDeviceCopyDone = true
		w.captureDeviceScreenshot(true)
	}
	if !allowInput || w.modal || d.busy || !d.ready || d.paused || d.failed || d.frameSize.X == 0 {
		w.cancelDeviceTouch()
		return
	}
	rect := w.deviceScreenRect()
	x, y := ebiten.CursorPosition()
	point := image.Pt(x, y)
	if point.In(rect) {
		w.cursorShape = ebiten.CursorShapePointer
	}
	mapPoint := func(p image.Point, bounds image.Rectangle, size image.Point) image.Point {
		return image.Pt(max(0, min((p.X-bounds.Min.X)*size.X/bounds.Dx(), size.X-1)), max(0, min((p.Y-bounds.Min.Y)*size.Y/bounds.Dy(), size.Y-1)))
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) && point.In(rect) {
		w.ui.ClearFocus()
		w.terminalFocus = false
		d.dragSize, d.dragRect = d.frameSize, rect
		d.lastTouch = mapPoint(point, rect, d.frameSize)
		d.dragging = w.sendDeviceControl(func(s *core.ScreenStream) error { return s.Touch(0, d.lastTouch, d.dragSize) })
	}
	if !d.dragging {
		return
	}
	if rect != d.dragRect || d.frameSize != d.dragSize {
		w.cancelDeviceTouch()
		return
	}
	next := mapPoint(point, rect, d.dragSize)
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
		d.dragging = false
		w.sendDeviceControl(func(s *core.ScreenStream) error { return s.Touch(1, next, d.dragSize) })
	} else if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && next != d.lastTouch {
		d.lastTouch = next
		if !w.sendDeviceControl(func(s *core.ScreenStream) error { return s.Touch(2, next, d.dragSize) }) {
			w.cancelDeviceTouch()
		}
	}
}

func (w *app) rebuildDeviceLayout() {
	previous := w.workspaceInputs()
	w.rebuild()
	for i, input := range w.workspaceInputs() {
		old := previous[i]
		if input == nil || old == nil {
			continue
		}
		input.TextInput.SetText(old.GetText())
		input.editText, input.undo, input.redo = old.editText, old.undo, old.redo
		input.editAt, input.editKind, input.editCursor = old.editAt, old.editKind, old.editCursor
		input.cursor, input.anchor, input.caretAt = old.cursor, old.anchor, old.caretAt
		input.scrollX = old.scrollX
	}
}
