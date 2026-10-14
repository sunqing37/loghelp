package ui

import (
	"github.com/ebitenui/ebitenui/input"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
)

func cursorOver(target *widget.Widget, x, y int) bool {
	return !target.Disabled && target.IsVisible() && target.In(x, y) &&
		target.EffectiveInputLayer().ActiveFor(x, y, input.LayerEventTypeAny)
}

func (w *app) hoverCursor(shape ebiten.CursorShapeType) widget.WidgetOpt {
	return widget.WidgetOpts.OnUpdate(func(target widget.HasWidget) {
		x, y := ebiten.CursorPosition()
		if cursorOver(target.GetWidget(), x, y) {
			w.cursorShape = shape
		}
	})
}

func (w *app) updatePanelCursor(p *page) {
	if w.tabDrag.dragging && !w.tabDrag.canceled {
		w.cursorShape = ebiten.CursorShapeEWResize
		return
	}
	if w.detailDragging {
		w.cursorShape = ebiten.CursorShapeNSResize
		return
	}
	if w.columnDrag.key != "" {
		w.cursorShape = ebiten.CursorShapeEWResize
		return
	}
	if w.modal || w.logPanel == nil {
		return
	}
	x, y := ebiten.CursorPosition()
	if p.shell == nil {
		if col := w.columnAtDivider(x, y); col.key != "" {
			w.cursorShape = ebiten.CursorShapeEWResize
			return
		}
	}
	if p.detailSelection.dragging || w.detailPanel != nil && p.selected >= 0 && p.selected < len(p.result.Rows) && cursorOver(w.detailPanel.GetWidget(), x, y) {
		w.cursorShape = ebiten.CursorShapeText
		return
	}
	if !cursorOver(w.logPanel.GetWidget(), x, y) {
		return
	}
	rect := w.panelRect()
	if x < rect.Min.X || x >= rect.Max.X || y < rect.Min.Y || y >= rect.Max.Y {
		return
	}
	if p.shell != nil {
		if p.shell.connection != nil {
			w.cursorShape = ebiten.CursorShapeText
		}
	} else if index := p.scroll + (y-rect.Min.Y)/w.rowHeight(); index >= 0 && index < len(p.result.Rows) {
		w.cursorShape = ebiten.CursorShapePointer
	}
}

// Apply the system cursor separately from the cached workspace image. This
// also preserves the shape while an input/button is held or dragged.
func (w *app) applyCursor() {
	if !ebiten.IsFocused() || w.backgroundDiagnostic || w.fileDialogOpen {
		return
	}
	x, y := ebiten.CursorPosition()
	if x < 0 || y < 0 || x >= w.width || y >= w.height {
		return
	}
	if ebiten.CursorShape() != w.cursorShape {
		ebiten.SetCursorShape(w.cursorShape)
	}
}
