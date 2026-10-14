package ui

import (
	"image"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

type tabWidget struct {
	page        *page
	title, body *widget.Widget
}

type tabDragState struct {
	page               *page
	start              image.Point
	dragging, canceled bool
}

// Track dragging before UI.Update so releasing a dragged title cannot also
// trigger its normal click handler. Commit only after UI.Update has finished.
func (w *app) updateTabDrag() {
	if w.modal || w.fileDialogOpen || w.tabScroll == nil {
		if w.tabDrag.page != nil {
			w.tabDrag.canceled = true
		}
		return
	}
	x, y := ebiten.CursorPosition()
	point := image.Pt(x, y)
	view := w.tabScroll.ViewRect()
	if w.tabDrag.page == nil && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) && point.In(view) {
		for _, tab := range w.tabWidgets {
			// Only the title starts a drag; the close button remains independent.
			if cursorOver(tab.title, x, y) {
				w.tabDrag = tabDragState{page: tab.page, start: point}
				break
			}
		}
	}
	drag := &w.tabDrag
	if drag.page == nil || drag.canceled {
		return
	}
	dx, dy := x-drag.start.X, y-drag.start.Y
	if dx*dx+dy*dy >= w.px(6)*w.px(6) {
		drag.dragging = true
	}
	if !drag.dragging || !point.In(view) || !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		return
	}
	// Reveal tabs beyond the viewport while the pointer is held near an edge.
	overflow := w.tabScroll.ContentRect().Dx() - view.Dx()
	if overflow > 0 {
		delta := 0.0
		if x < view.Min.X+w.px(24) {
			delta = -float64(w.px(8))
		} else if x >= view.Max.X-w.px(24) {
			delta = float64(w.px(8))
		}
		w.tabScroll.ScrollLeft = max(0, min(w.tabScroll.ScrollLeft+delta/float64(overflow), 1))
	}
}

// A slot is an insertion boundary in the current order, before removal.
func (w *app) tabDropSlot(x, y int) (slot, marker int, ok bool) {
	if w.tabScroll == nil || len(w.tabWidgets) == 0 || !image.Pt(x, y).In(w.tabScroll.ViewRect()) {
		return 0, 0, false
	}
	for i, tab := range w.tabWidgets {
		rect := tab.body.Rect
		if x < rect.Min.X+rect.Dx()/2 {
			return i, rect.Min.X, true
		}
	}
	return len(w.tabWidgets), w.tabWidgets[len(w.tabWidgets)-1].body.Rect.Max.X, true
}

func (w *app) finishTabDrag() {
	drag := w.tabDrag
	if drag.page == nil || ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		return
	}
	w.tabDrag = tabDragState{}
	if !drag.dragging || drag.canceled || w.modal || drag.page.dead {
		return
	}
	x, y := ebiten.CursorPosition()
	slot, _, ok := w.tabDropSlot(x, y)
	if !ok {
		return
	}
	for i, p := range w.pages {
		if p == drag.page {
			if i < slot {
				slot--
			}
			w.moveTab(p, slot)
			return
		}
	}
}

func (w *app) moveTab(p *page, target int) {
	if p == nil || p.dead || target < 0 || target >= len(w.pages) {
		return
	}
	for source, candidate := range w.pages {
		if candidate != p {
			continue
		}
		if source == target {
			return
		}
		w.syncInputs()
		active := w.active()
		if source < target {
			copy(w.pages[source:target], w.pages[source+1:target+1])
		} else {
			copy(w.pages[target+1:source+1], w.pages[target:source])
		}
		w.pages[target] = p
		for i, candidate := range w.pages {
			if candidate == active {
				w.selected = i
				break
			}
		}
		w.rebuild()
		w.save()
		return
	}
}

func (w *app) drawTabDrag(screen *ebiten.Image) {
	if !w.tabDrag.dragging || w.tabDrag.canceled || w.modal || w.fileDialogOpen || !ebiten.IsFocused() {
		return
	}
	x, y := ebiten.CursorPosition()
	_, marker, ok := w.tabDropSlot(x, y)
	if !ok {
		return
	}
	view := w.tabScroll.ViewRect().Intersect(screen.Bounds())
	if view.Empty() {
		return
	}
	clip := screen.SubImage(view).(*ebiten.Image)
	for _, tab := range w.tabWidgets {
		if tab.page == w.tabDrag.page {
			rect := tab.body.Rect
			vector.StrokeRect(clip, float32(rect.Min.X+w.px(1)), float32(rect.Min.Y+w.px(1)), float32(rect.Dx()-w.px(2)), float32(rect.Dy()-w.px(2)), float32(w.px(2)), w.colors().accent, false)
			break
		}
	}
	marker = max(view.Min.X+w.px(1), min(marker, view.Max.X-w.px(2)))
	vector.FillRect(clip, float32(marker-w.px(1)), float32(view.Min.Y+w.px(2)), float32(w.px(3)), float32(max(view.Dy()-w.px(4), 0)), w.colors().accent, false)
}
