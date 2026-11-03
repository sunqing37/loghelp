package vt10x

import "strings"

type textPosition struct{ row, col int }
type textSelection struct {
	anchor, caret textPosition
	active        bool
}

func (p textPosition) before(other textPosition) bool {
	return p.row < other.row || p.row == other.row && p.col < other.col
}

func (s textSelection) bounds() (textPosition, textPosition) {
	if s.caret.before(s.anchor) {
		return s.caret, s.anchor
	}
	return s.anchor, s.caret
}

func (t *State) selectionPosition(x, y int) textPosition {
	history, offset := t.Scrollback()
	return textPosition{history - offset + clamp(y, 0, t.rows-1), clamp(x, 0, t.cols)}
}

func (t *State) BeginSelection(x, y int) {
	position := t.selectionPosition(x, y)
	t.selection = textSelection{position, position, true}
}

func (t *State) ExtendSelection(x, y int) {
	if t.selection.active {
		t.selection.caret = t.selectionPosition(x, y)
	}
}

func (t *State) ClearSelection() { t.selection = textSelection{} }

func (t *State) HasSelection() bool {
	return t.selection.active && t.selection.anchor != t.selection.caret
}

func (t *State) SelectionRange(y int) (int, int) {
	if !t.selection.active {
		return 0, 0
	}
	start, end := t.selection.bounds()
	row := t.selectionPosition(0, y).row
	if row < start.row || row > end.row {
		return 0, 0
	}
	left, right := 0, t.cols
	if row == start.row {
		left = start.col
	}
	if row == end.row {
		right = end.col
	}
	return left, right
}

func (t *State) SelectionText() (string, bool) {
	start, end := t.selection.bounds()
	if !t.selection.active || start == end {
		return "", false
	}
	history, _ := t.Scrollback()
	var text strings.Builder
	for index := start.row; index <= end.row; index++ {
		var row line
		if index < history {
			row = t.history[(t.historyStart+index)%historyLimit]
		} else {
			row = t.lines[index-history]
		}
		left, right := 0, min(len(row), t.cols)
		if index == start.row {
			left = min(start.col, right)
		}
		if index == end.row {
			right = min(end.col, right)
		}
		var part strings.Builder
		for _, cell := range row[left:right] {
			char := cell.Char
			if char == 0 {
				char = ' '
			}
			part.WriteRune(char)
		}
		// Soft wraps are one logical line. Explicit line breaks trim padding.
		wrapped := right == len(row) && right > 0 && row[right-1].Mode&attrWrap != 0
		if index < end.row && !wrapped {
			text.WriteString(strings.TrimRight(part.String(), " "))
			text.WriteByte('\n')
		} else {
			text.WriteString(part.String())
		}
	}
	return text.String(), true
}
