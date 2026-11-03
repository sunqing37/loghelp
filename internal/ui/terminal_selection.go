package ui

import (
	"image"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Drawing, hit testing and caret boundaries use the same terminal cell metrics.
func (w *app) terminalCellSize() (float64, float64) {
	cw, _ := text.Measure("M", w.face, 0)
	return max(cw, float64(w.px(7))), w.fontSize() + float64(w.px(4))
}

func (w *app) hasTerminalSelection(p *page) bool {
	if p.shell == nil || p.shell.vt == nil {
		return false
	}
	p.shell.vt.Lock()
	defer p.shell.vt.Unlock()
	return p.shell.vt.HasSelection()
}

func (w *app) terminalMouse(p *page, x, y int) {
	s := p.shell
	if s.vt == nil {
		return
	}
	rect := w.panelRect()
	rect.Max.X -= w.px(14) // The scrollbar owns this strip, even without history.
	point := image.Pt(x, y)
	inside := point.In(rect)
	if inside && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) && w.hasTerminalSelection(p) {
		w.copyTerminal(p)
		return
	}
	cw, ch := w.terminalCellSize()
	col := max(0, min(int(math.Round(float64(x-rect.Min.X)/cw)), s.cols))
	row := max(0, min(int(math.Floor(float64(y-rect.Min.Y)/ch)), s.rows-1))
	if inside && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		w.ui.ClearFocus()
		w.terminalFocus = true
		s.vt.Lock()
		s.vt.BeginSelection(col, row)
		s.vt.Unlock()
		s.selecting, s.selectionPoint, s.selectionScrollAt = true, point, time.Now()
		w.renderDirty = true
	}
	scrolled := false
	_, wheel := ebiten.Wheel()
	if point.In(w.panelRect()) && wheel != 0 {
		steps := int(math.Copysign(math.Max(math.Abs(wheel*3), 1), wheel))
		s.vt.Scroll(steps)
		scrolled = true
	}
	if !s.selecting {
		return
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && time.Since(s.selectionScrollAt) >= 70*time.Millisecond {
		if y < rect.Min.Y {
			s.vt.Scroll(1)
			scrolled = true
		} else if y >= rect.Max.Y {
			s.vt.Scroll(-1)
			scrolled = true
		}
		s.selectionScrollAt = time.Now()
	}
	if point != s.selectionPoint || scrolled {
		s.vt.Lock()
		s.vt.ExtendSelection(col, row)
		s.vt.Unlock()
		s.selectionPoint = point
		w.renderDirty = true
	}
	if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		s.selecting = false
	}
}
