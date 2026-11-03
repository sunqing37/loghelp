package vt10x

// Bound both row count and cell memory, including history made at wider sizes.
const historyLimit = 10000
const historyCellLimit = 1 << 20

func (t *State) clearHistory() {
	t.ClearSelection()
	t.history = nil
	t.historyStart, t.historyCount, t.historyCells, t.scrollOffset = 0, 0, 0, 0
}

func (t *State) rememberLine(row line) {
	if t.history == nil {
		t.history = make([]line, historyLimit)
	}
	if t.scrollOffset > 0 {
		t.scrollOffset++ // Keep the viewed text anchored while new output arrives.
	}
	for t.historyCount > 0 && (t.historyCount == historyLimit || t.historyCells+len(row) > historyCellLimit) {
		if t.selection.active {
			t.selection.anchor.row--
			t.selection.caret.row--
			if t.selection.anchor.row < 0 || t.selection.caret.row < 0 {
				t.ClearSelection()
			}
		}
		t.historyCells -= len(t.history[t.historyStart])
		t.history[t.historyStart] = nil
		t.historyStart = (t.historyStart + 1) % historyLimit
		t.historyCount--
	}
	// Terminal sizes are bounded by the window; still enforce the memory limit.
	if len(row) <= historyCellLimit {
		t.history[(t.historyStart+t.historyCount)%historyLimit] = append(line(nil), row...)
		t.historyCount++
		t.historyCells += len(row)
	}
	t.scrollOffset = clamp(t.scrollOffset, 0, t.historyCount)
}

func (t *State) Scrollback() (lines, offset int) {
	if t.mode&ModeAltScreen != 0 {
		return 0, 0
	}
	return t.historyCount, t.scrollOffset
}

func (t *State) Scroll(lines int) {
	t.lock()
	defer t.unlock()
	if t.mode&ModeAltScreen == 0 {
		t.scrollOffset = clamp(t.scrollOffset+lines, 0, t.historyCount)
	}
}

func (t *State) ScrollTo(offset int) {
	t.lock()
	defer t.unlock()
	if t.mode&ModeAltScreen == 0 {
		t.scrollOffset = clamp(offset, 0, t.historyCount)
	}
}

func (t *State) ViewCell(x, y int) Glyph {
	_, offset := t.Scrollback()
	index := t.historyCount - offset + y
	if offset == 0 || index >= t.historyCount {
		return t.Cell(x, y-offset)
	}
	row := t.history[(t.historyStart+index)%historyLimit]
	if x >= len(row) {
		return Glyph{Char: ' ', FG: DefaultFG, BG: DefaultBG}
	}
	cell := row[x]
	if fg, ok := t.colorOverride[cell.FG]; ok {
		cell.FG = fg
	}
	if bg, ok := t.colorOverride[cell.BG]; ok {
		cell.BG = bg
	}
	return cell
}
