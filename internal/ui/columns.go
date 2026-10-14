package ui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

type columnResizeState struct {
	key            string
	anchorX, width int
	changed        bool
}

func minColumnWidth(key string) int {
	switch key {
	case "level":
		return 42
	case "message":
		return 160
	default:
		return 60
	}
}

func (w *app) columnWidth(key string, fallback int) int {
	if width := w.config.ColumnWidths[key]; width > 0 {
		return w.px(max(minColumnWidth(key), min(width, 4096)))
	}
	return fallback
}

// Both drawing and hit testing use table coordinates, including horizontal scroll.
func (w *app) columnAtDivider(x, y int) logColumn {
	if w.rowHeader == nil || !cursorOver(w.rowHeader.GetWidget(), x, y) {
		return logColumn{}
	}
	header := w.rowHeader.GetWidget().Rect
	position := w.panelRect().Min.X - w.tableX
	for _, col := range w.logColumns(w.panelRect().Dx()) {
		position += col.width
		if position > header.Min.X && position <= header.Max.X && x >= position-w.px(5) && x <= position+w.px(5) {
			return col
		}
	}
	return logColumn{}
}

func (w *app) finishColumnResize() {
	changed := w.columnDrag.changed
	w.columnDrag = columnResizeState{}
	if changed {
		w.save()
	}
}

func (w *app) resizeColumns(x, y int) bool {
	if w.columnDrag.key != "" {
		drag := &w.columnDrag
		w.cursorShape = ebiten.CursorShapeEWResize
		width := max(minColumnWidth(drag.key), min(w.logical(drag.width+x-drag.anchorX), 4096))
		if x != drag.anchorX || drag.changed {
			if w.config.ColumnWidths == nil {
				w.config.ColumnWidths = map[string]int{}
			}
			if w.config.ColumnWidths[drag.key] != width {
				w.config.ColumnWidths[drag.key] = width
				drag.changed = true
				w.renderDirty = true
			}
		}
		if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			w.finishColumnResize()
		}
		return true
	}
	if col := w.columnAtDivider(x, y); col.key != "" && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		w.ui.ClearFocus()
		w.columnDrag = columnResizeState{key: col.key, anchorX: x, width: col.width}
		w.cursorShape = ebiten.CursorShapeEWResize
		return true
	}
	if w.rowHeader != nil && cursorOver(w.rowHeader.GetWidget(), x, y) && ebiten.IsKeyPressed(ebiten.KeyShift) {
		_, wheel := ebiten.Wheel()
		if wheel != 0 {
			width := w.panelRect().Dx()
			w.tableX = max(0, min(w.tableX-int(wheel*float64(w.px(55))), max(w.tableWidth(width)-width, 0)))
			return true
		}
	}
	return false
}
