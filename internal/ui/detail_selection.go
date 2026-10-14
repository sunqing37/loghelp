package ui

import (
	"image"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Positions are byte offsets in the displayed source, independent of soft
// wrapping and viewport height. No additional copy of the log is retained.
type detailSelection struct {
	anchor, caret     int
	focused, dragging bool
	scrollAt          time.Time
	lastPoint         image.Point
	lastScroll, lastX int
}

func (selection detailSelection) bounds() (int, int) {
	return min(selection.anchor, selection.caret), max(selection.anchor, selection.caret)
}

func (w *app) prepareDetails(p *page) bool {
	if w.detailPanel == nil || p == nil || p.selected < 0 || p.selected >= len(p.result.Rows) {
		return false
	}
	w.detailLines(p, p.detailEntry(p.selected), max(w.detailPanel.GetWidget().Rect.Dx()-w.px(40), 1))
	return true
}

func (w *app) selectedDetailText(p *page) string {
	if !w.prepareDetails(p) {
		return ""
	}
	start, end := p.detailSelection.bounds()
	raw := p.detailCache.entry.Raw
	if start < 0 || end > len(raw) || start == end {
		return ""
	}
	return raw[start:end]
}

func (w *app) copyDetailSelection() {
	if value := w.selectedDetailText(w.active()); value != "" {
		if err := clipboard.WriteAll(value); err != nil {
			w.message("剪贴板", err.Error())
		}
	}
}

func (w *app) detailKeys(p *page) bool {
	if !p.detailSelection.focused || w.ui.HasFocus() || p.shell != nil || !modifierDown("mod") {
		return false
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyA) {
		if w.prepareDetails(p) {
			p.detailSelection.anchor, p.detailSelection.caret = 0, len(p.detailCache.entry.Raw)
			w.renderDirty = true
		}
		return true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyC) {
		w.copyDetailSelection()
		return true
	}
	return false
}

func (w *app) detailCaret(p *page, x, y int) int {
	rect := w.detailPanel.GetWidget().Rect
	cache := &p.detailCache
	lineHeight := w.detailLineHeight()
	visible := max((rect.Dy()-w.px(24))/lineHeight, 1)
	row := max(0, min(p.detailScroll+max(0, min(y-rect.Min.Y-w.px(12), (visible-1)*lineHeight))/lineHeight, len(cache.lines)-1))
	line := cache.lines[row]
	target := float64(max(x-rect.Min.X-w.px(16)+p.detailX, 0))
	if target == 0 || len(line) == 0 {
		return cache.offsets[row]
	}
	// Limit probing to the portion near the pointer even when wrapping is off
	// and the original log contains a very long physical line.
	limit := max(int(target/w.fontSize()*2)+16, 1)
	ends := make([]int, 1, min(len(line), limit)+1)
	for offset := 0; offset < len(line) && len(ends) <= limit; {
		_, size := utf8.DecodeRuneInString(line[offset:])
		offset += size
		ends = append(ends, offset)
	}
	left, right := 0, len(ends)-1
	for left < right {
		middle := (left + right + 1) / 2
		width, _ := text.Measure(line[:ends[middle]], w.face, 0)
		if width <= target {
			left = middle
		} else {
			right = middle - 1
		}
	}
	if left+1 < len(ends) {
		a, _ := text.Measure(line[:ends[left]], w.face, 0)
		b, _ := text.Measure(line[:ends[left+1]], w.face, 0)
		if target-a >= b-target {
			left++
		}
	}
	return cache.offsets[row] + ends[left]
}

func (w *app) selectDetails(p *page, x, y int) bool {
	if !w.prepareDetails(p) {
		return false
	}
	selection := &p.detailSelection
	rect := w.detailPanel.GetWidget().Rect
	point := image.Pt(x, y)
	inside := cursorOver(w.detailPanel.GetWidget(), x, y)
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		if !inside {
			selection.focused = false
			return false
		}
		w.ui.ClearFocus()
		selection.focused, selection.dragging = true, true
		position := w.detailCaret(p, x, y)
		if !ebiten.IsKeyPressed(ebiten.KeyShift) {
			selection.anchor = position
		}
		selection.caret = position
		selection.lastPoint, selection.lastScroll, selection.lastX = point, p.detailScroll, p.detailX
		w.renderDirty = true
	}
	if !selection.dragging {
		return false
	}
	w.cursorShape = ebiten.CursorShapeText
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && time.Since(selection.scrollAt) >= 70*time.Millisecond {
		lineHeight := w.detailLineHeight()
		visible := max((rect.Dy()-w.px(24))/lineHeight, 1)
		if y < rect.Min.Y+w.px(12) {
			p.detailScroll = max(0, p.detailScroll-1)
		} else if y >= rect.Max.Y-w.px(12) {
			p.detailScroll = min(p.detailScroll+1, max(len(p.detailCache.lines)-visible, 0))
		}
		selection.scrollAt = time.Now()
	}
	if point != selection.lastPoint || p.detailScroll != selection.lastScroll || p.detailX != selection.lastX {
		selection.caret = w.detailCaret(p, x, y)
		selection.lastPoint, selection.lastScroll, selection.lastX = point, p.detailScroll, p.detailX
		w.renderDirty = true
	}
	if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		selection.dragging = false
	}
	return true
}

func (w *app) drawDetailSelection(screen *ebiten.Image, p *page, row int, x, y float64) {
	start, end := p.detailSelection.bounds()
	if start == end {
		return
	}
	cache := &p.detailCache
	line, offset := cache.lines[row], cache.offsets[row]
	left, right := max(start-offset, 0), min(end-offset, len(line))
	newline := offset+len(line) < len(cache.entry.Raw) && cache.entry.Raw[offset+len(line)] == '\n' && start <= offset+len(line) && end > offset+len(line)
	if left >= right && !newline {
		return
	}
	left, right = min(left, len(line)), max(right, 0)
	prefix, _ := text.Measure(line[:left], w.face, 0)
	width, _ := text.Measure(line[left:right], w.face, 0)
	if newline {
		space, _ := text.Measure(" ", w.face, 0)
		width += space
	}
	c := w.colors()
	bg, fg := c.selection, c.text
	if p.detailSelection.focused {
		bg = c.accent
		fg = rgb(255, 255, 255)
		if !w.lightTheme() {
			fg = c.bg
		}
	}
	vector.FillRect(screen, float32(x+prefix), float32(y-float64(w.px(2))), float32(width), float32(w.detailLineHeight()), bg, false)
	w.paint(screen, line[left:right], x+prefix, y, fg)
}
