package ui

import (
	"image"
	"strings"
	"time"
	"unicode"

	uiinput "github.com/ebitenui/ebitenui/input"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

var inputLineBreaks = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ")

// These are single-line controls; line breaks would make prefix widths
// non-monotonic and leave part of pasted text outside the visible line.
func singleLineInput(value string) string { return inputLineBreaks.Replace(value) }

func inputWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_'
}

// Chinese uses a continuous text span, without guessing dictionary word breaks.
// Spaces and punctuation prefer the nearest word; symbol-only inputs still select.
func inputWordRange(runes []rune, position int) (int, int) {
	if len(runes) == 0 {
		return 0, 0
	}
	position = max(0, min(position, len(runes)-1))
	if !inputWordRune(runes[position]) {
		for distance := 1; distance < len(runes); distance++ {
			left, right := position-distance, position+distance
			if left >= 0 && inputWordRune(runes[left]) {
				position = left
				break
			}
			if right < len(runes) && inputWordRune(runes[right]) {
				position = right
				break
			}
		}
	}
	start, end := position, position+1
	word := inputWordRune(runes[position])
	for start > 0 && inputWordRune(runes[start-1]) == word && (word || unicode.IsSpace(runes[start-1]) == unicode.IsSpace(runes[position])) {
		start--
	}
	for end < len(runes) && inputWordRune(runes[end]) == word && (word || unicode.IsSpace(runes[end]) == unicode.IsSpace(runes[position])) {
		end++
	}
	return start, end
}

func (input *boundedInput) selectedRange() (int, int) {
	length := len([]rune(input.GetText()))
	input.cursor = max(0, min(input.cursor, length))
	if input.anchor >= 0 {
		input.anchor = min(input.anchor, length)
	}
	if input.anchor < 0 {
		return input.cursor, input.cursor
	}
	return min(input.cursor, input.anchor), max(input.cursor, input.anchor)
}

func (input *boundedInput) SelectedText() string {
	start, end := input.selectedRange()
	return string([]rune(input.GetText())[start:end])
}

func (input *boundedInput) SelectAll() {
	input.anchor, input.cursor = 0, len([]rune(input.GetText()))
	input.dragging, input.wordDragging = false, false
	input.clickAt = time.Time{}
	input.editKind = ""
	input.caretAt = time.Now()
}

func (input *boundedInput) replaceSelection(value, kind string) {
	if input.GetWidget().Disabled {
		return
	}
	value = singleLineInput(value)
	runes := []rune(input.GetText())
	start, end := input.selectedRange()
	cursor := start + len([]rune(value))
	result := string(runes[:start]) + value + string(runes[end:])
	input.TextInput.SetText(result)
	input.recordEditAt(input.GetText(), kind, start, end, cursor)
	input.cursor, input.anchor = cursor, -1
	input.dragging, input.wordDragging = false, false
	input.clickAt = time.Time{}
	input.caretAt = time.Now()
}

func (input *boundedInput) Insert(value string) { input.replaceSelection(value, "") }

func (input *boundedInput) DeleteSelectedText() {
	if start, end := input.selectedRange(); start != end {
		input.replaceSelection("", "")
	}
}

func (input *boundedInput) prefixWidth(runes []rune, position int) float64 {
	width, _ := text.Measure(string(runes[:position]), *input.face, 0)
	return width
}

func (input *boundedInput) hitCaret(x int) int {
	runes := []rune(input.GetText())
	target := float64(x-input.GetWidget().Rect.Min.X-input.padding.Left) - input.scrollX
	left, right := 0, len(runes)
	for left < right {
		middle := (left + right) / 2
		if input.prefixWidth(runes, middle) < target {
			left = middle + 1
		} else {
			right = middle
		}
	}
	if left > 0 && target < (input.prefixWidth(runes, left-1)+input.prefixWidth(runes, left))/2 {
		left--
	}
	return left
}

func inputKeyRepeated(key ebiten.Key) bool {
	duration := inpututil.KeyPressDuration(key)
	return duration == 1 || duration > 24 && (duration-24)%3 == 0
}

func (input *boundedInput) moveCaret(position int, extend bool) {
	input.clickAt = time.Time{}
	if extend {
		if input.anchor < 0 {
			input.anchor = input.cursor
		}
	} else {
		input.anchor = -1
	}
	input.cursor = max(0, min(position, len([]rune(input.GetText()))))
	input.editKind = ""
	input.caretAt = time.Now()
}

func (input *boundedInput) updateEditing() {
	x, y := ebiten.CursorPosition()
	point := image.Pt(x, y)
	if input.GetWidget().Disabled {
		input.dragging = false
		input.clickAt = time.Time{}
		return
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		if cursorOver(input.GetWidget(), x, y) {
			position := input.hitCaret(x)
			delta := point.Sub(input.clickPosition)
			double := !input.clickAt.IsZero() && time.Since(input.clickAt) <= 350*time.Millisecond &&
				delta.X*delta.X+delta.Y*delta.Y <= 25 &&
				!modifierDown("shift") && !modifierDown("mod") && !modifierDown("alt")
			input.dragging, input.wordDragging = true, double
			if double {
				input.wordStart, input.wordEnd = inputWordRange([]rune(input.GetText()), position)
				input.anchor, input.cursor = input.wordStart, input.wordEnd
				input.clickAt = time.Time{}
				input.clickPosition = point
			} else {
				input.moveCaret(position, modifierDown("shift"))
				if input.anchor < 0 {
					input.anchor = input.cursor
				}
				input.clickAt, input.clickPosition = time.Now(), point
			}
			input.editKind = ""
			input.caretAt = time.Now()
		} else {
			input.clickAt = time.Time{}
			input.dragging = false
		}
	}
	if input.dragging {
		if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) || inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
			delta := point.Sub(input.clickPosition)
			if !input.wordDragging && delta.X*delta.X+delta.Y*delta.Y > 25 {
				input.clickAt = time.Time{}
			}
			position := input.hitCaret(x)
			if input.wordDragging {
				start, end := inputWordRange([]rune(input.GetText()), position)
				if delta.X*delta.X+delta.Y*delta.Y <= 25 {
					// Revealing the selected word may scroll text under a stationary
					// pointer. Only physical dragging should expand that selection.
					input.anchor, input.cursor = input.wordStart, input.wordEnd
				} else if position < input.wordStart {
					input.anchor, input.cursor = input.wordEnd, start
				} else if position > input.wordEnd {
					input.anchor, input.cursor = input.wordStart, end
				} else {
					input.anchor, input.cursor = input.wordStart, input.wordEnd
				}
			} else {
				input.cursor = position
			}
		}
		if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			input.dragging = false
		}
	}
	if !input.IsFocused() {
		// A first click receives focus through a deferred EbitenUI event.
		if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			input.dragging = false
		}
		input.editKind = ""
		return
	}
	mod, shift := modifierDown("mod"), modifierDown("shift")
	if !mod {
		if chars := uiinput.InputChars(); len(chars) > 0 {
			input.replaceSelection(string(chars), "typing")
			return
		}
	}
	runes := []rune(input.GetText())
	start, end := input.selectedRange()
	for _, key := range []ebiten.Key{ebiten.KeyArrowLeft, ebiten.KeyArrowRight, ebiten.KeyHome, ebiten.KeyEnd} {
		if !inputKeyRepeated(key) {
			continue
		}
		position := input.cursor
		switch key {
		case ebiten.KeyHome:
			position = 0
		case ebiten.KeyEnd:
			position = len(runes)
		case ebiten.KeyArrowLeft:
			position--
			if start != end && !shift {
				position = start
			} else if mod {
				for position > 0 && !inputWordRune(runes[position]) {
					position--
				}
				for position > 0 && inputWordRune(runes[position-1]) {
					position--
				}
			}
		case ebiten.KeyArrowRight:
			position++
			if start != end && !shift {
				position = end
			} else if mod {
				for position < len(runes) && !inputWordRune(runes[position]) {
					position++
				}
				for position < len(runes) && inputWordRune(runes[position]) {
					position++
				}
			}
		}
		input.moveCaret(position, shift)
		input.dragging = false
		return
	}
	if inputKeyRepeated(ebiten.KeyBackspace) {
		if start == end && input.cursor > 0 {
			input.anchor = input.cursor - 1
		}
		input.replaceSelection("", "backspace")
	} else if inputKeyRepeated(ebiten.KeyDelete) {
		if start == end && input.cursor < len(runes) {
			input.anchor = input.cursor + 1
		}
		input.replaceSelection("", "delete")
	} else if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		input.editKind = ""
		input.TextInput.Submit()
	}
}

func (input *boundedInput) drawInput(screen *ebiten.Image, rect image.Rectangle) {
	background := input.images.Idle
	if input.GetWidget().Disabled {
		background = input.images.Disabled
	}
	background.Draw(screen, rect.Dx(), rect.Dy(), nil)
	body := input.padding.Apply(rect).Intersect(rect)
	if body.Empty() {
		return
	}
	canvas := screen.SubImage(body).(*ebiten.Image)
	start, end := input.selectedRange()
	runes := []rune(input.GetText())
	caretX := input.prefixWidth(runes, input.cursor)
	width := input.prefixWidth(runes, len(runes))
	if input.IsFocused() {
		input.scrollX = min(input.scrollX, float64(body.Dx()-1)-caretX)
		input.scrollX = max(input.scrollX, -caretX)
	}
	input.scrollX = max(min(input.scrollX, 0), min(0, float64(body.Dx()-1)-width))
	x, y := float64(body.Min.X)+input.scrollX, float64(body.Min.Y)
	_, height := text.Measure(" ", *input.face, 0)
	value, foreground := input.GetText(), input.foreground
	if value == "" {
		value, foreground = input.placeholder, input.muted
	} else if input.GetWidget().Disabled {
		foreground = input.muted
	}
	options := &text.DrawOptions{}
	options.GeoM.Translate(x, y)
	options.ColorScale.ScaleWithColor(foreground)
	text.Draw(canvas, value, *input.face, options)
	if start != end {
		left, right := input.prefixWidth(runes, start), input.prefixWidth(runes, end)
		vector.FillRect(canvas, float32(x+left), float32(y), float32(right-left), float32(height), rgb(6, 67, 161), false)
		selected := &text.DrawOptions{}
		selected.GeoM.Translate(x+left, y)
		selected.ColorScale.ScaleWithColor(rgb(255, 255, 255))
		text.Draw(canvas, string(runes[start:end]), *input.face, selected)
	}
	if input.IsFocused() && !input.GetWidget().Disabled && time.Since(input.caretAt)%(time.Second) < 500*time.Millisecond {
		vector.FillRect(canvas, float32(x+caretX), float32(y), 1, float32(height), input.foreground, false)
	}
}
