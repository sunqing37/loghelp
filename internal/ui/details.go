package ui

import (
	"image"
	"strings"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"loghelp/internal/core"
)

const (
	minDetailBody       = 72
	minLogBody          = 96
	detailDividerHeight = 12
)

type detailTextCache struct {
	entry    *core.Entry
	width    int
	fontSize float32
	wrapped  bool
	lines    []string
	offsets  []int
	search   core.Search
	matches  [][]int
}

func (w *app) detailLines(p *page, entry *core.Entry, width int) []string {
	cache := &p.detailCache
	if cache.entry != entry || cache.width != width || cache.fontSize != w.config.FontSize || cache.wrapped != w.config.Wrap {
		if cache.entry == nil || cache.entry.Seq != entry.Seq || !strings.HasPrefix(entry.Raw, cache.entry.Raw) {
			p.detailSelection = detailSelection{}
		}
		*cache = detailTextCache{entry: entry, width: width, fontSize: w.config.FontSize, wrapped: w.config.Wrap, lines: w.wrap(entry.Raw, float64(width))}
		position := 0
		for i, line := range cache.lines {
			if i > 0 && position < len(entry.Raw) && entry.Raw[position] == '\n' {
				position++
			}
			cache.offsets = append(cache.offsets, position)
			position += len(line)
		}
	}
	if cache.search != p.search {
		cache.search = p.search
		cache.matches = p.search.Matches(entry.Raw)
	}
	return cache.lines
}

// Layout the shared log/detail space from its actual available height, keeping
// both panes usable even when the window is smaller than the saved preference.
type detailSplitLayout struct{ app *app }

func (layout *detailSplitLayout) PreferredSize(children []widget.PreferredSizeLocateableWidget) (int, int) {
	width, height := 0, 0
	for _, child := range children {
		cw, ch := child.PreferredSize()
		width = max(width, cw)
		height += ch
	}
	return width, height
}

func (layout *detailSplitLayout) Layout(children []widget.PreferredSizeLocateableWidget, rect image.Rectangle) {
	if len(children) != 3 {
		return
	}
	_, groupMinimum := children[2].PreferredSize()
	headerHeight := groupMinimum - minDetailBody
	maximum := max(minDetailBody, rect.Dy()-minLogBody-detailDividerHeight-headerHeight)
	bodyHeight := max(minDetailBody, min(layout.app.config.DetailHeight, maximum))
	lowerHeight := headerHeight + bodyHeight
	dividerY := rect.Max.Y - lowerHeight - detailDividerHeight
	children[0].SetLocation(image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, dividerY))
	children[1].SetLocation(image.Rect(rect.Min.X, dividerY, rect.Max.X, dividerY+detailDividerHeight))
	children[2].SetLocation(image.Rect(rect.Min.X, dividerY+detailDividerHeight, rect.Max.X, rect.Max.Y))
	layout.app.detailMaxHeight = maximum
}

func (w *app) finishDetailResize() {
	changed := w.detailDragChanged
	w.detailDragging, w.detailDragChanged = false, false
	if changed {
		w.save()
	}
}

func (w *app) resizeDetails(x, y int) bool {
	if w.detailArea == nil || w.detailSplitter == nil || w.detailPanel == nil {
		return false
	}
	if w.detailDragging {
		w.cursorShape = ebiten.CursorShapeNSResize
		if y != w.detailAnchorY || w.detailDragChanged {
			height := max(minDetailBody, min(w.detailAnchorHeight+w.detailAnchorY-y, w.detailMaxHeight))
			if height != w.config.DetailHeight {
				w.config.DetailHeight = height
				w.detailDragChanged = true
				w.renderDirty = true
				w.detailArea.RequestRelayout()
			}
		}
		if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			w.finishDetailResize()
		}
		return true
	}
	if cursorOver(w.detailSplitter.GetWidget(), x, y) && inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		w.detailDragging = true
		w.detailAnchorY = y
		w.detailAnchorHeight = w.detailPanel.GetWidget().Rect.Dy()
		w.cursorShape = ebiten.CursorShapeNSResize
		return true
	}
	return false
}
