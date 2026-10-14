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

type palette struct{ bg, surface, elevated, sidebar, text, muted, line, accent, accentSoft, selection, success, danger color.NRGBA }

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }
func (w *app) colors() palette {
	if w.lightTheme() {
		return palette{rgb(238, 242, 247), rgb(255, 255, 255), rgb(246, 248, 252), rgb(248, 250, 253), rgb(35, 48, 68), rgb(110, 125, 147), rgb(222, 229, 239), rgb(48, 105, 225), rgb(232, 240, 255), rgb(225, 235, 253), rgb(30, 146, 107), rgb(207, 67, 81)}
	}
	return palette{rgb(15, 20, 29), rgb(23, 30, 42), rgb(30, 39, 54), rgb(19, 26, 37), rgb(227, 233, 244), rgb(137, 152, 176), rgb(43, 55, 74), rgb(115, 157, 255), rgb(33, 49, 80), rgb(31, 48, 75), rgb(90, 203, 159), rgb(247, 113, 128)}
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
func (w *app) action(s, kind string, fn func()) *widget.Button {
	c := w.colors()
	idle, hover, pressed, border, fg := c.elevated, c.selection, c.accentSoft, c.line, c.text
	switch kind {
	case "primary":
		idle, hover, pressed, border, fg = c.accent, rgb(95, 137, 241), rgb(58, 101, 200), c.accent, rgb(255, 255, 255)
	case "quiet":
		idle, border = c.surface, c.surface
	case "active":
		idle, border, fg = c.accentSoft, c.accentSoft, c.accent
	case "danger":
		fg = c.danger
	}
	return widget.NewButton(widget.ButtonOpts.WidgetOpts(widget.WidgetOpts.MinSize(w.px(32), w.px(controlHeight)), w.hoverCursor(ebiten.CursorShapePointer)), widget.ButtonOpts.Image(&widget.ButtonImage{Idle: w.skin(idle, border, 6), Hover: w.skin(hover, c.line, 6), Pressed: w.skin(pressed, c.accent, 6), Disabled: w.skin(c.surface, c.line, 6)}), widget.ButtonOpts.Text(s, &w.uiFace, &widget.ButtonTextColor{Idle: fg, Hover: fg, Pressed: fg, Disabled: c.muted}), widget.ButtonOpts.TextPadding(&widget.Insets{Left: w.px(12), Right: w.px(12), Top: w.px(8), Bottom: w.px(8)}), widget.ButtonOpts.ClickedHandler(func(*widget.ButtonClickedEventArgs) { fn() }))
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
