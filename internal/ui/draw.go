package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/hinshun/vt10x"
	"loghelp/internal/core"
)

func (w *app) paint(screen *ebiten.Image, s string, x, y float64, c color.Color) {
	opts := &text.DrawOptions{}
	opts.GeoM.Translate(x, y)
	opts.ColorScale.ScaleWithColor(c)
	text.Draw(screen, s, w.face, opts)
}
func (w *app) highlight(screen *ebiten.Image, s string, x, y float64, search core.Search, base color.Color) {
	w.highlightSpans(screen, s, x, y, search.Matches(s), 0, base)
}

func (w *app) highlightSpans(screen *ebiten.Image, s string, x, y float64, spans [][]int, start int, base color.Color) {
	cursor := 0
	draw := func(part string, highlight bool) {
		if part == "" {
			return
		}
		width, _ := text.Measure(part, w.face, 0)
		c := base
		if highlight {
			vector.FillRect(screen, float32(x), float32(y), float32(width), float32(w.fontSize()+float64(w.px(3))), rgb(234, 184, 91), false)
			c = rgb(35, 42, 55)
		}
		w.paint(screen, part, x, y, c)
		x += width
	}
	for _, span := range spans {
		if span[1] <= start {
			continue
		}
		if span[0] >= start+len(s) {
			break
		}
		left, right := max(span[0]-start, 0), min(span[1]-start, len(s))
		if left < cursor || left == right {
			continue
		}
		draw(s[cursor:left], false)
		draw(s[left:right], true)
		cursor = right
	}
	draw(s[cursor:], false)
}
func entryColor(e *core.Entry) color.NRGBA {
	switch core.LevelValue(e.Level) {
	case 1, 2:
		return color.NRGBA{94, 153, 218, 255}
	case 3:
		return color.NRGBA{61, 168, 119, 255}
	case 4:
		return color.NRGBA{211, 145, 33, 255}
	case 5, 6:
		return color.NRGBA{224, 71, 83, 255}
	}
	return color.NRGBA{145, 156, 172, 255}
}
func (w *app) metadata(e *core.Entry) string {
	fields := []string{e.Level}
	for _, column := range w.config.Columns {
		switch column {
		case "time":
			if !e.Time.IsZero() {
				fields = append(fields, e.Time.Local().Format("01-02 15:04:05.000"))
			}
		case "pid":
			fields = append(fields, fmt.Sprint(e.PID))
		case "tid":
			fields = append(fields, fmt.Sprint(e.TID))
		case "tag":
			fields = append(fields, e.Tag)
		case "package":
			pkg := e.Package
			if pkg == "" {
				pkg = "未知包名"
			}
			fields = append(fields, pkg)
		case "process":
			fields = append(fields, e.Process)
		}
	}
	return strings.Join(fields, "  ")
}

type logColumn struct {
	key, title string
	width      int
}

func (w *app) logColumns(width int) []logColumn {
	scale := float64(w.config.FontSize) / 14
	levelWidth := w.columnWidth("level", w.px(42))
	cols := []logColumn{{"level", "级别", levelWidth}}
	used := levelWidth
	names := map[string]string{"time": "时间", "pid": "PID", "tid": "TID", "tag": "TAG", "package": "应用", "process": "进程"}
	widths := map[string]int{"time": 190, "pid": 84, "tid": 84, "tag": 174, "package": 224, "process": 220}
	for _, key := range w.config.Columns {
		if n := widths[key]; n > 0 {
			n = w.px(int(float64(n) * scale))
			n = w.columnWidth(key, n)
			cols = append(cols, logColumn{key, names[key], n})
			used += n
		}
	}
	cols = append(cols, logColumn{"message", "消息", w.columnWidth("message", max(width-used-w.px(12), w.px(320)))})
	return cols
}
func (w *app) tableWidth(width int) int {
	total := 0
	for _, col := range w.logColumns(width) {
		total += col.width
	}
	return total
}
func cellValue(e *core.Entry, key string) string {
	switch key {
	case "time":
		if !e.Time.IsZero() {
			return e.Time.Local().Format("01-02 15:04:05.000")
		}
		return "—"
	case "pid":
		if e.PID > 0 {
			return fmt.Sprint(e.PID)
		}
		return "—"
	case "tid":
		if e.TID > 0 {
			return fmt.Sprint(e.TID)
		}
		return "—"
	case "tag":
		return e.Tag
	case "package":
		if e.Package != "" {
			return e.Package
		}
		return "—"
	case "process":
		if e.Process != "" {
			return e.Process
		}
		return "—"
	case "message":
		return strings.ReplaceAll(e.Message, "\n", " ↵ ")
	}
	return e.Level
}
func (w *app) paintFace(screen *ebiten.Image, s string, x, y float64, face text.Face, c color.Color) {
	opts := &text.DrawOptions{}
	opts.GeoM.Translate(x, y)
	opts.ColorScale.ScaleWithColor(c)
	text.Draw(screen, s, face, opts)
}
func (w *app) fit(s string, width float64) string {
	if measured, _ := text.Measure(s, w.face, 0); measured <= width {
		return s
	}
	r := []rune(s)
	left, right := 0, len(r)
	for left < right {
		mid := (left + right + 1) / 2
		measure, _ := text.Measure(string(r[:mid])+"…", w.face, 0)
		if measure <= width {
			left = mid
		} else {
			right = mid - 1
		}
	}
	return string(r[:left]) + "…"
}
func (w *app) drawPanels(screen *ebiten.Image) {
	defer func() {
		if w.modal {
			vector.FillRect(screen, 0, 0, float32(w.width), float32(w.height), color.NRGBA{0, 0, 0, 145}, false)
		}
	}()
	p := w.active()
	if p == nil || w.logPanel == nil {
		return
	}
	c := w.colors()
	rect := w.panelRect().Intersect(screen.Bounds())
	if rect.Empty() {
		return
	}
	area := screen.SubImage(rect).(*ebiten.Image)
	area.Fill(c.bg)
	if p.shell != nil {
		w.drawTerminal(area, p, rect)
		return
	}
	w.drawDetailDivider(screen)
	cols := w.logColumns(rect.Dx())
	if w.columnDrag.key == "" {
		w.tableX = max(0, min(w.tableX, w.tableWidth(rect.Dx())-rect.Dx()))
	}
	if w.rowHeader != nil {
		header := w.rowHeader.GetWidget().Rect.Intersect(screen.Bounds())
		if !header.Empty() {
			canvas := screen.SubImage(header).(*ebiten.Image)
			canvas.Fill(c.surface)
			_, headerTextHeight := text.Measure(" ", w.smallFace, 0)
			headerTextY := float64(header.Min.Y) + (float64(header.Dy())-headerTextHeight)/2
			x := rect.Min.X - w.tableX
			mx, my := ebiten.CursorPosition()
			hover := w.columnAtDivider(mx, my)
			for _, col := range cols {
				r := image.Rect(x, header.Min.Y, x+col.width, header.Max.Y).Intersect(header)
				if !r.Empty() {
					w.paintFace(canvas.SubImage(r).(*ebiten.Image), col.title, float64(x+w.px(12)), headerTextY, w.smallFace, c.muted)
				}
				x += col.width
				lineColor := c.line
				if !w.modal && (w.columnDrag.key == col.key || hover.key == col.key) {
					lineColor = c.accent
				}
				vector.FillRect(canvas, float32(x-w.px(1)), float32(header.Min.Y+w.px(5)), float32(w.px(1)), float32(max(header.Dy()-w.px(10), 1)), lineColor, false)
			}
			vector.FillRect(canvas, float32(header.Min.X), float32(header.Max.Y-w.px(1)), float32(header.Dx()), float32(w.px(1)), c.line, false)
		}
	}
	rows := w.visibleRows()
	p.viewportRows = rows
	if p.follow && !p.paused {
		p.scroll = max(len(p.result.Rows)-rows, 0)
	}
	p.scroll = max(0, min(p.scroll, max(len(p.result.Rows)-rows, 0)))
	mx, my := ebiten.CursorPosition()
	_, rowTextHeight := text.Measure(" ", w.face, 0)
	for index := p.scroll; index < len(p.result.Rows) && index < p.scroll+rows; index++ {
		e := p.result.Rows[index]
		top := rect.Min.Y + (index-p.scroll)*w.rowHeight()
		y := float64(top) + (float64(w.rowHeight())-rowTextHeight)/2
		selected := index == p.selected
		if selected {
			vector.FillRect(area, float32(rect.Min.X), float32(top), float32(rect.Dx()), float32(w.rowHeight()), c.selection, false)
			vector.FillRect(area, float32(rect.Min.X), float32(top), float32(w.px(3)), float32(w.rowHeight()), c.accent, false)
		} else if !w.modal && mx >= rect.Min.X && mx < rect.Max.X && my >= top && my < top+w.rowHeight() {
			vector.FillRect(area, float32(rect.Min.X), float32(top), float32(rect.Dx()), float32(w.rowHeight()), c.surface, false)
		}
		x := rect.Min.X - w.tableX
		for _, col := range cols {
			cellRect := image.Rect(x, top, x+col.width-w.px(8), min(top+w.rowHeight(), rect.Max.Y)).Intersect(rect)
			if !cellRect.Empty() {
				canvas := area.SubImage(cellRect).(*ebiten.Image)
				value := cellValue(e, col.key)
				fg := c.muted
				if col.key == "message" {
					fg = c.text
					runes := []rune(value)
					if len(runes) > 500 {
						value = string(runes[:500]) + "…"
					}
					messageX := float64(x + w.px(12))
					if start, end := p.rowRange(index); end-start > 1 {
						prefix := fmt.Sprintf("[%d 行] ", end-start)
						w.paint(canvas, prefix, messageX, y, c.muted)
						prefixWidth, _ := text.Measure(prefix, w.face, 0)
						messageX += prefixWidth
					}
					w.highlight(canvas, value, messageX, y, p.search, fg)
				} else if col.key == "level" {
					fg = entryColor(e)
					if w.lightTheme() {
						switch core.LevelValue(e.Level) {
						case 1, 2:
							fg = rgb(40, 103, 192)
						case 3:
							fg = rgb(20, 132, 97)
						case 4:
							fg = rgb(162, 106, 15)
						case 5, 6:
							fg = rgb(193, 53, 78)
						}
					}
					vector.FillRect(canvas, float32(x+w.px(9)), float32(top+w.px(7)), float32(w.px(23)), float32(w.fontSize()+float64(w.px(4))), color.NRGBA{fg.R, fg.G, fg.B, 24}, false)
					w.paint(canvas, value, float64(x+w.px(15)), y, fg)
				} else {
					if col.key == "tag" {
						fg = c.text
					}
					w.paint(canvas, w.fit(value, float64(col.width-w.px(22))), float64(x+w.px(12)), y, fg)
				}
			}
			x += col.width
		}
	}
	if len(p.result.Rows) == 0 {
		title, hint := "开始一次日志分析", "在左侧选择设备并开始采集，或打开已有日志文件"
		if p.collector != nil || p.offline != nil {
			title, hint = "没有符合条件的日志", "尝试调整过滤条件，或等待设备产生新日志"
		}
		tw, _ := text.Measure(title, w.titleFace, 0)
		hw, _ := text.Measure(hint, w.uiFace, 0)
		cy := float64(rect.Min.Y+rect.Dy()/2) - float64(w.px(35))
		w.paintFace(area, title, float64(rect.Min.X)+float64(rect.Dx())/2-tw/2, cy, w.titleFace, c.text)
		w.paintFace(area, hint, float64(rect.Min.X)+float64(rect.Dx())/2-hw/2, cy+float64(w.px(38)), w.uiFace, c.muted)
	}
	if total := len(p.result.Rows); total > rows {
		track := rect.Dy()
		height := max(track*rows/total, w.px(28))
		offset := (track - height) * p.scroll / max(total-rows, 1)
		vector.FillRect(area, float32(rect.Max.X-w.px(5)), float32(rect.Min.Y+offset), float32(w.px(3)), float32(height), c.line, false)
	}
	if w.detailPanel == nil {
		return
	}
	detailRect := w.detailPanel.GetWidget().Rect.Intersect(screen.Bounds())
	if detailRect.Empty() {
		return
	}
	detail := screen.SubImage(detailRect).(*ebiten.Image)
	detail.Fill(c.surface)
	vector.FillRect(detail, float32(detailRect.Min.X), float32(detailRect.Min.Y), float32(detailRect.Dx()), float32(w.px(1)), c.line, false)
	if p.selected < 0 || p.selected >= len(p.result.Rows) {
		p.detailCache = detailTextCache{}
		p.detailSelection = detailSelection{}
		p.groupDetail = nil
		p.groupDetailSource = nil
		w.paintFace(detail, "点击日志查看完整内容、复制原文或展开上下文", float64(detailRect.Min.X+w.px(16)), float64(detailRect.Min.Y+w.px(16)), w.uiFace, c.muted)
		return
	}
	e := p.detailEntry(p.selected)
	lines := w.detailLines(p, e, max(detailRect.Dx()-w.px(40), 1))
	lineHeight := w.detailLineHeight()
	visible := max((detailRect.Dy()-w.px(24))/lineHeight, 1)
	if w.config.Wrap {
		p.detailX = 0
	}
	p.detailScroll = max(0, min(p.detailScroll, max(len(lines)-visible, 0)))
	for i := p.detailScroll; i < len(lines) && i < p.detailScroll+visible; i++ {
		w.highlightSpans(detail, lines[i], float64(detailRect.Min.X+w.px(16)-p.detailX), float64(detailRect.Min.Y+w.px(12)+(i-p.detailScroll)*lineHeight), p.detailCache.matches, p.detailCache.offsets[i], c.text)
		w.drawDetailSelection(detail, p, i, float64(detailRect.Min.X+w.px(16)-p.detailX), float64(detailRect.Min.Y+w.px(12)+(i-p.detailScroll)*lineHeight))
	}
	if len(lines) > visible {
		track := max(detailRect.Dy()-w.px(24), 1)
		height := min(track, max(w.px(18), track*visible/len(lines)))
		offset := (track - height) * p.detailScroll / max(len(lines)-visible, 1)
		vector.FillRect(detail, float32(detailRect.Max.X-w.px(6)), float32(detailRect.Min.Y+w.px(12)+offset), float32(w.px(3)), float32(height), c.muted, false)
	}
}

func (w *app) drawDetailDivider(screen *ebiten.Image) {
	if w.detailSplitter == nil {
		return
	}
	rect := w.detailSplitter.GetWidget().Rect.Intersect(screen.Bounds())
	if rect.Empty() {
		return
	}
	c := w.colors()
	grip := c.muted
	x, y := ebiten.CursorPosition()
	if !w.modal && (w.detailDragging || cursorOver(w.detailSplitter.GetWidget(), x, y)) {
		grip = c.accent
	}
	cy := rect.Min.Y + rect.Dy()/2
	vector.FillRect(screen, float32(rect.Min.X), float32(cy), float32(rect.Dx()), float32(w.px(1)), c.line, false)
	vector.FillRect(screen, float32(rect.Min.X+rect.Dx()/2-w.px(20)), float32(cy-w.px(1)), float32(w.px(40)), float32(w.px(3)), grip, false)
}

func (w *app) wrap(s string, width float64) []string {
	if !w.config.Wrap {
		return strings.Split(s, "\n")
	}
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		maxRunes := max(int(width/w.fontSize()*2)+8, 1)
		ends := make([]int, 1, maxRunes+1)
		for len(line) > 0 {
			ends = ends[:1]
			for offset := 0; offset < len(line) && len(ends) <= maxRunes; {
				_, size := utf8.DecodeRuneInString(line[offset:])
				offset += size
				ends = append(ends, offset)
			}
			left, right := 1, len(ends)-1
			for left < right {
				middle := (left + right + 1) / 2
				measured, _ := text.Measure(line[:ends[middle]], w.face, 0)
				if measured <= width {
					left = middle
				} else {
					right = middle - 1
				}
			}
			// Keep slices of the original UTF-8 text, avoiding a copy of a
			// potentially large log line on every wrap or height adjustment.
			lines = append(lines, line[:ends[left]])
			line = line[ends[left]:]
		}
	}
	return lines
}

func (w *app) mouse(p *page) {
	if w.modal || w.tabDrag.page != nil {
		return
	}
	x, y := ebiten.CursorPosition()
	if p.shell == nil && w.resizeColumns(x, y) {
		return
	}
	if w.resizeDetails(x, y) {
		return
	}
	if w.selectDetails(p, x, y) {
		return
	}
	point := image.Pt(x, y)
	_, wheel := ebiten.Wheel()
	rect := w.panelRect()
	if point.In(rect) {
		if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			w.ui.ClearFocus()
			if p.shell != nil {
				w.terminalFocus = true
			} else {
				index := p.scroll + (y-rect.Min.Y)/w.rowHeight()
				if index < len(p.result.Rows) {
					p.detailSelection = detailSelection{}
					p.selectRow(index)
					p.detailScroll = 0
					p.detailX = 0
					p.follow = false
				}
			}
		}
		if wheel != 0 && p.shell == nil {
			if ebiten.IsKeyPressed(ebiten.KeyShift) {
				w.tableX = max(0, min(w.tableX-int(wheel*float64(w.px(55))), max(w.tableWidth(rect.Dx())-rect.Dx(), 0)))
				return
			}
			p.follow = false
			p.keepSelectionVisible = false
			p.scroll = max(0, min(p.scroll-int(wheel*3), max(len(p.result.Rows)-w.visibleRows(), 0)))
		}
	}
	if w.detailPanel != nil && point.In(w.detailPanel.GetWidget().Rect) && wheel != 0 {
		if ebiten.IsKeyPressed(ebiten.KeyShift) && !w.config.Wrap {
			p.detailX = max(0, p.detailX-int(wheel*float64(w.px(40))))
		} else {
			p.detailScroll = max(0, p.detailScroll-int(wheel*3))
		}
	}
}

var ansiPalette = []color.NRGBA{{25, 29, 37, 255}, {205, 69, 77, 255}, {61, 167, 105, 255}, {210, 167, 47, 255}, {76, 126, 209, 255}, {168, 99, 199, 255}, {62, 167, 181, 255}, {193, 200, 209, 255}, {95, 107, 120, 255}, {244, 104, 115, 255}, {112, 218, 156, 255}, {238, 200, 91, 255}, {128, 176, 240, 255}, {207, 151, 231, 255}, {121, 214, 229, 255}, {244, 247, 249, 255}}

func vtColor(c vt10x.Color, foreground bool) color.NRGBA {
	if c == vt10x.DefaultFG || c == vt10x.DefaultBG || c == vt10x.DefaultCursor {
		if foreground {
			return ansiPalette[7]
		}
		return ansiPalette[0]
	}
	if c < 16 {
		return ansiPalette[int(c)]
	}
	if c < 232 {
		n := int(c) - 16
		values := []uint8{0, 95, 135, 175, 215, 255}
		return color.NRGBA{values[n/36], values[(n/6)%6], values[n%6], 255}
	}
	if c < 256 {
		v := uint8(8 + (c-232)*10)
		return color.NRGBA{v, v, v, 255}
	}
	return color.NRGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255}
}
func (w *app) drawTerminal(screen *ebiten.Image, p *page, rect image.Rectangle) {
	state := p.shell
	if state.vt == nil {
		return
	}
	cw, _ := text.Measure("M", w.face, 0)
	cw = max(cw, float64(w.px(7)))
	ch := w.fontSize() + float64(w.px(4))
	cols, rows := max(int(float64(rect.Dx())/cw), 2), max(int(float64(rect.Dy())/ch), 2)
	if cols != state.cols || rows != state.rows {
		state.cols, state.rows = cols, rows
		state.vt.Resize(cols, rows)
		if state.connection != nil {
			conn := state.connection
			go conn.Resize(uint(rows), uint(cols))
		}
	}
	state.vt.Lock()
	defer state.vt.Unlock()
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := state.vt.Cell(col, row)
			x, y := float64(rect.Min.X)+float64(col)*cw, float64(rect.Min.Y)+float64(row)*ch
			bg := vtColor(cell.BG, false)
			vector.FillRect(screen, float32(x), float32(y), float32(cw), float32(ch), bg, false)
			if cell.Char >= 32 {
				w.paint(screen, string(cell.Char), x, y, vtColor(cell.FG, true))
			}
		}
	}
	if w.terminalFocus && state.vt.CursorVisible() && time.Now().UnixMilli()%1000 < 600 {
		cursor := state.vt.Cursor()
		vector.FillRect(screen, float32(float64(rect.Min.X)+float64(cursor.X)*cw), float32(float64(rect.Min.Y)+float64(cursor.Y+1)*ch-float64(w.px(2))), float32(cw), float32(w.px(2)), color.White, false)
	}
}
