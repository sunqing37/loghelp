package ui

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

type logScrollDrag struct {
	page   *page
	detail bool
	grab   int
}

type logScrollGeometry struct {
	track, thumb image.Rectangle
	maximum      int
}

// Drawing and input share the same thumb size, range and usable track.
func (w *app) logScrollbar(p *page, detail bool) logScrollGeometry {
	rect, total, visible, offset := w.panelRect(), len(p.result.Rows), w.visibleRows(), p.scroll
	if detail {
		if w.detailPanel == nil || p.selected < 0 || p.selected >= len(p.result.Rows) {
			return logScrollGeometry{}
		}
		rect = w.detailPanel.GetWidget().Rect
		lines := w.detailLines(p, p.detailEntry(p.selected), max(rect.Dx()-w.px(40), 1))
		total, visible, offset = len(lines), max((rect.Dy()-w.px(24))/w.detailLineHeight(), 1), p.detailScroll
		rect.Min.Y += w.px(12)
		rect.Max.Y -= w.px(12)
	}
	if p.shell != nil || total <= visible || rect.Empty() {
		return logScrollGeometry{}
	}
	track := image.Rect(max(rect.Min.X, rect.Max.X-w.px(14)), rect.Min.Y, rect.Max.X, rect.Max.Y)
	height := min(track.Dy(), max(w.px(28), int(int64(track.Dy())*int64(visible)/int64(total))))
	maximum := total - visible
	top := track.Min.Y + int(int64(track.Dy()-height)*int64(max(0, min(offset, maximum)))/int64(maximum))
	return logScrollGeometry{track, image.Rect(track.Min.X+w.px(3), top, track.Max.X-w.px(3), top+height), maximum}
}

func (w *app) drawLogScrollbar(screen *ebiten.Image, p *page, detail bool) {
	g := w.logScrollbar(p, detail)
	if g.track.Empty() {
		return
	}
	c := w.colors()
	x, y := ebiten.CursorPosition()
	thumb := c.muted
	if !w.modal && (image.Pt(x, y).In(g.track) || w.logScrollDrag.page == p && w.logScrollDrag.detail == detail) {
		thumb = c.accent
	}
	vector.FillRect(screen, float32(g.track.Min.X), float32(g.track.Min.Y), float32(g.track.Dx()), float32(g.track.Dy()), c.surface, false)
	vector.FillRect(screen, float32(g.thumb.Min.X), float32(g.thumb.Min.Y), float32(g.thumb.Dx()), float32(g.thumb.Dy()), thumb, false)
}

func (w *app) scrollLogs(p *page, x, y int) bool {
	drag := &w.logScrollDrag
	if drag.page != nil {
		if drag.page != p || p.dead || !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			*drag = logScrollDrag{}
			return true
		}
		g := w.logScrollbar(p, drag.detail)
		travel := g.track.Dy() - g.thumb.Dy()
		if !g.track.Empty() && travel > 0 {
			offset := int(int64(max(0, min(y-g.track.Min.Y-drag.grab, travel))) * int64(g.maximum) / int64(travel))
			if drag.detail {
				p.detailScroll = offset
			} else {
				p.scroll = offset
			}
			w.renderDirty = true
		}
		return true
	}
	for _, detail := range []bool{false, true} {
		g := w.logScrollbar(p, detail)
		if g.track.Empty() || !image.Pt(x, y).In(g.track) {
			continue
		}
		if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			w.ui.ClearFocus()
			p.detailSelection.dragging = false
			if !detail {
				p.follow, p.keepSelectionVisible = false, false
			}
			grab := y - g.thumb.Min.Y
			if !image.Pt(x, y).In(g.thumb) {
				grab = g.thumb.Dy() / 2
			}
			*drag = logScrollDrag{p, detail, grab}
			return w.scrollLogs(p, x, y)
		}
		// Reserve left clicks and right clicks for the scrollbar; wheel still
		// reaches the normal scrolling path.
		return inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight)
	}
	return false
}
