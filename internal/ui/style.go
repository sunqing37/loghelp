package ui

import (
	"image"
	"image/color"
	"math"

	uiimage "github.com/ebitenui/ebitenui/image"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Keep text, control boundaries and action colors separate from decorative dividers.
type palette struct {
	bg, surface, elevated, sidebar, stripe, input    color.NRGBA
	text, muted, line, controlBorder                 color.NRGBA
	accent, accentSoft, selection                    color.NRGBA
	primary, primaryHover, primaryPressed, onPrimary color.NRGBA
	success, successSoft, danger, dangerSoft         color.NRGBA
	highlight, onHighlight                           color.NRGBA
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }
func (w *app) colors() palette {
	if w.lightTheme() {
		return palette{
			bg: rgb(239, 243, 248), surface: rgb(255, 255, 255), elevated: rgb(242, 246, 251), sidebar: rgb(248, 250, 253), stripe: rgb(247, 249, 252), input: rgb(255, 255, 255),
			text: rgb(26, 39, 59), muted: rgb(80, 98, 122), line: rgb(213, 222, 234), controlBorder: rgb(128, 144, 166),
			accent: rgb(33, 88, 192), accentSoft: rgb(233, 241, 255), selection: rgb(219, 233, 255),
			primary: rgb(37, 92, 201), primaryHover: rgb(29, 78, 178), primaryPressed: rgb(25, 66, 151), onPrimary: rgb(255, 255, 255),
			success: rgb(16, 112, 80), successSoft: rgb(225, 245, 235), danger: rgb(185, 43, 67), dangerSoft: rgb(255, 235, 239),
			highlight: rgb(255, 224, 145), onHighlight: rgb(56, 40, 9),
		}
	}
	return palette{
		bg: rgb(13, 20, 31), surface: rgb(21, 30, 43), elevated: rgb(31, 44, 62), sidebar: rgb(17, 26, 38), stripe: rgb(25, 36, 51), input: rgb(13, 22, 35),
		text: rgb(232, 239, 249), muted: rgb(163, 179, 200), line: rgb(48, 64, 85), controlBorder: rgb(104, 125, 151),
		accent: rgb(142, 183, 255), accentSoft: rgb(33, 54, 84), selection: rgb(37, 63, 96),
		primary: rgb(42, 91, 192), primaryHover: rgb(47, 99, 205), primaryPressed: rgb(34, 77, 165), onPrimary: rgb(255, 255, 255),
		success: rgb(111, 222, 174), successSoft: rgb(23, 61, 49), danger: rgb(255, 152, 166), dangerSoft: rgb(75, 34, 47),
		highlight: rgb(246, 204, 111), onHighlight: rgb(43, 32, 10),
	}
}

type skinKey struct {
	fill, border color.NRGBA
	radius       int
	scale        float64
}

func (w *app) skin(fill, border color.NRGBA, radius int) *uiimage.NineSlice {
	if w.skins == nil {
		w.skins = map[skinKey]*uiimage.NineSlice{}
	}
	key := skinKey{fill, border, radius, w.displayScale}
	if s := w.skins[key]; s != nil {
		return s
	}
	radius = w.px(radius)
	pad := radius + w.px(2)
	size := 2*pad + 1
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	half := float64(size) / 2
	clamp := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := math.Abs(float64(x)+0.5-half) - half + float64(radius)
			dy := math.Abs(float64(y)+0.5-half) - half + float64(radius)
			d := math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) + math.Min(math.Max(dx, dy), 0) - float64(radius)
			a := clamp(0.5 - d)
			inside := clamp(0.5 - float64(w.px(1)) - d)
			if a == 0 {
				continue
			}
			f := inside / a
			img.SetNRGBA(x, y, color.NRGBA{uint8(float64(fill.R)*f + float64(border.R)*(1-f)), uint8(float64(fill.G)*f + float64(border.G)*(1-f)), uint8(float64(fill.B)*f + float64(border.B)*(1-f)), uint8(float64(fill.A)*inside + float64(border.A)*(a-inside))})
		}
	}
	s := uiimage.NewNineSlice(ebiten.NewImageFromImage(img), [3]int{pad, 1, pad}, [3]int{pad, 1, pad})
	w.skins[key] = s
	return s
}
func (w *app) textLabel(s string, face *text.Face, c color.Color) *widget.Text {
	return widget.NewText(widget.TextOpts.Text(s, face, c), widget.TextOpts.Position(widget.TextPositionStart, widget.TextPositionCenter))
}
func (w *app) muted(s string) *widget.Text   { return w.textLabel(s, &w.smallFace, w.colors().muted) }
func (w *app) heading(s string) *widget.Text { return w.textLabel(s, &w.titleFace, w.foreground()) }
func (w *app) buttonStyle(kind string) (*widget.ButtonImage, *widget.ButtonTextColor) {
	c := w.colors()
	idle, hover, pressed, border, fg := c.elevated, c.selection, c.accentSoft, c.controlBorder, c.text
	switch kind {
	case "primary":
		idle, hover, pressed, border, fg = c.primary, c.primaryHover, c.primaryPressed, c.primary, c.onPrimary
	case "quiet", "tab":
		idle, border = color.NRGBA{}, color.NRGBA{}
	case "tab-active":
		idle, border, fg = color.NRGBA{}, color.NRGBA{}, c.accent
	case "active":
		idle, border, fg = c.accentSoft, c.accent, c.accent
	case "capture":
		idle, hover, pressed, border, fg = c.successSoft, c.successSoft, c.selection, c.success, c.success
	case "danger":
		idle, hover, pressed, border, fg = c.dangerSoft, c.dangerSoft, c.selection, c.danger, c.danger
	}
	images := &widget.ButtonImage{
		Idle: w.skin(idle, border, 6), Hover: w.skin(hover, c.accent, 6), Pressed: w.skin(pressed, c.accent, 6), Disabled: w.skin(c.elevated, c.line, 6),
	}
	colors := &widget.ButtonTextColor{Idle: fg, Hover: fg, Pressed: fg, Disabled: c.muted}
	return images, colors
}
func (w *app) action(s, kind string, fn func()) *widget.Button {
	images, colors := w.buttonStyle(kind)
	button := widget.NewButton(widget.ButtonOpts.WidgetOpts(widget.WidgetOpts.MinSize(w.px(32), w.px(controlHeight)), w.hoverCursor(ebiten.CursorShapePointer)), widget.ButtonOpts.Image(images), widget.ButtonOpts.Text(s, &w.uiFace, colors), widget.ButtonOpts.TextPadding(&widget.Insets{Left: w.px(10), Right: w.px(10), Top: w.px(6), Bottom: w.px(6)}), widget.ButtonOpts.ClickedHandler(func(*widget.ButtonClickedEventArgs) { fn() }))
	button.GetWidget().CustomData = colors
	return button
}
func (w *app) restyleAction(button *widget.Button, kind string) {
	images, colors := w.buttonStyle(kind)
	button.SetImage(images)
	if current, ok := button.GetWidget().CustomData.(*widget.ButtonTextColor); ok {
		*current = *colors
	}
}

// Cards group a task without adding heavy visual chrome to every control.
func (w *app) card(content widget.PreferredSizeLocateableWidget, pad int) *widget.Container {
	box := w.inset(content, pad, w.colors().surface)
	box.SetBackgroundImage(w.skin(w.colors().surface, w.colors().line, 8))
	return box
}
func (w *app) inset(content widget.PreferredSizeLocateableWidget, pad int, fill color.NRGBA) *widget.Container {
	box := widget.NewContainer(widget.ContainerOpts.BackgroundImage(uiimage.NewNineSliceColor(fill)), widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1), widget.GridLayoutOpts.Stretch([]bool{true}, []bool{true}), widget.GridLayoutOpts.Padding(widget.NewInsetsSimple(w.px(pad))))))
	box.AddChild(content)
	return box
}
func (w *app) stretchRow(left, right widget.PreferredSizeLocateableWidget) *widget.Container {
	box := widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(2), widget.GridLayoutOpts.Stretch([]bool{true, false}, nil), widget.GridLayoutOpts.Spacing(w.px(12), 0))))
	box.AddChild(left, right)
	return box
}

func brandIcon(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(size) / 32
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)/scale, float64(y)/scale
			cx, cy := math.Max(6-px, math.Max(px-25, 0)), math.Max(6-py, math.Max(py-25, 0))
			if math.Hypot(cx, cy) > 6 {
				continue
			}
			shade := rgb(50, 91, 185)
			if px >= 9 && px < 13 && py >= 7 && py < 25 || px >= 9 && px < 24 && py >= 21 && py < 25 {
				shade = rgb(242, 247, 255)
			}
			img.SetNRGBA(x, y, shade)
		}
	}
	return img
}
