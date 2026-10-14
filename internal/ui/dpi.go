package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// UI constants and saved dimensions are device-independent pixels. Widget
// rectangles, drawing surfaces and Ebitengine pointer positions use pixels.
func (w *app) px(value int) int {
	return int(math.Round(float64(value) * w.displayScale))
}

func (w *app) logical(value int) int {
	return int(math.Round(float64(value) / w.displayScale))
}

func (w *app) fontSize() float64     { return float64(w.config.FontSize) * w.displayScale }
func (w *app) detailLineHeight() int { return w.px(int(w.config.FontSize) + 8) }

func monitorScale() float64 {
	scale := ebiten.Monitor().DeviceScaleFactor()
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 1
	}
	return scale
}

// LayoutF avoids truncating fractional window sizes before converting to
// physical pixels. Ebitengine also maps the pointer into this pixel space.
func (w *app) LayoutF(width, height float64) (float64, float64) {
	scale := monitorScale()
	// A dialog can contain unsaved fields (including a hidden parent dialog).
	// Keep its scale until it closes; likewise do not rebuild an active editor
	// or interrupt a drag when moving the window between monitors.
	changeScale := scale != w.displayScale && !w.modal && !w.fileDialogOpen &&
		!w.ui.HasFocus() && !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) &&
		w.columnDrag.key == "" && !w.detailDragging && w.tabDrag.page == nil
	var inputs []*boundedInput
	if changeScale {
		// Keep unsubmitted text and editing history without syncing device fields:
		// syncInputs can stop a collector when the serial field was edited.
		inputs = w.workspaceInputs()
		ratio := scale / w.displayScale
		w.tableX = int(math.Round(float64(w.tableX) * ratio))
		for _, p := range w.pages {
			p.detailX = int(math.Round(float64(p.detailX) * ratio))
		}
		w.displayScale = scale
		w.loadFace()
	}
	width, height = max(width, 1000)*w.displayScale, max(height, 720)*w.displayScale
	w.width, w.height = int(math.Ceil(width)), int(math.Ceil(height))
	if changeScale {
		w.rebuild()
		for i, input := range w.workspaceInputs() {
			previous := inputs[i]
			if input == nil || previous == nil {
				continue
			}
			input.TextInput.SetText(previous.GetText())
			input.editText, input.undo, input.redo = previous.editText, previous.undo, previous.redo
			input.editAt, input.editKind, input.editCursor = previous.editAt, previous.editKind, previous.editCursor
			input.cursor, input.anchor, input.caretAt = previous.cursor, previous.anchor, previous.caretAt
			input.scrollX = previous.scrollX * scale / previous.displayScale
		}
	}
	return width, height
}

func (w *app) workspaceInputs() []*boundedInput {
	return []*boundedInput{w.serialInput, w.appInput, w.buffersInput, w.filterInput, w.searchInput, w.commandInput}
}
