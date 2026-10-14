package ui

import (
	"image"
	"image/color"
	"time"
	"unicode/utf8"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"loghelp/internal/diagnostics"
)

// boundedInput shares EbitenUI's layout and focus, while rendering its editable
// text into an input-sized image rather than workspace-sized masking buffers.
type boundedInput struct {
	*widget.TextInput
	buffer                 *ebiten.Image
	legacyDiagnostic       bool
	editText               string
	undo, redo             []inputEdit
	editAt                 time.Time
	editKind               string
	editCursor             int
	cursor, anchor         int
	scrollX                float64
	face                   *text.Face
	images                 *widget.TextInputImage
	padding                widget.Insets
	foreground, muted      color.Color
	clickAt                time.Time
	clickPosition          image.Point
	dragging, wordDragging bool
	wordStart, wordEnd     int
	placeholder            string
	caretAt                time.Time
}

type inputEdit struct {
	text   string
	cursor int
}

// Keep editing history local to the control and bound retained text, including
// when a user pastes a large log into a search or command field.
func pushInputEdit(stack []inputEdit, edit inputEdit) []inputEdit {
	const maxBytes = 1 << 20
	if len(edit.text) > maxBytes {
		return nil
	}
	stack = append(stack, edit)
	bytes, start := 0, len(stack)
	for start > 0 && len(stack)-start < 100 {
		if bytes+len(stack[start-1].text) > maxBytes {
			break
		}
		start--
		bytes += len(stack[start].text)
	}
	copy(stack, stack[start:])
	clear(stack[len(stack)-start:])
	return stack[:len(stack)-start]
}

// Rune-wise edit spans identify adjacent edits for undo grouping.
func inputEditSpan(before, after string) (start, oldEnd, newEnd int) {
	oldRunes, newRunes := []rune(before), []rune(after)
	for start < len(oldRunes) && start < len(newRunes) && oldRunes[start] == newRunes[start] {
		start++
	}
	oldEnd, newEnd = len(oldRunes), len(newRunes)
	for oldEnd > start && newEnd > start && oldRunes[oldEnd-1] == newRunes[newEnd-1] {
		oldEnd--
		newEnd--
	}
	return
}

func (input *boundedInput) recordEdit(value, kind string) {
	if value == input.editText {
		return
	}
	start, oldEnd, newEnd := inputEditSpan(input.editText, value)
	input.recordEditAt(value, kind, start, oldEnd, newEnd)
}

// Use the actual replacement range for user edits. Comparing text alone cannot
// locate insertion/deletion reliably within runs such as "aaaa".
func (input *boundedInput) recordEditAt(value, kind string, start, oldEnd, newEnd int) {
	if value == input.editText {
		return
	}
	contiguous := kind == "typing" && oldEnd == start && start == input.editCursor ||
		kind == "backspace" && newEnd == start && oldEnd == input.editCursor ||
		kind == "delete" && newEnd == start && start == input.editCursor
	grouped := kind != "" && kind == input.editKind && contiguous && time.Since(input.editAt) < 750*time.Millisecond
	if !grouped {
		input.undo = pushInputEdit(input.undo, inputEdit{text: input.editText, cursor: input.cursor})
	}
	input.redo = nil
	input.editText = value
	input.editAt, input.editKind, input.editCursor = time.Now(), kind, newEnd
}

func (input *boundedInput) SetText(value string) {
	value = singleLineInput(value)
	input.recordEdit(input.GetText(), "")
	input.TextInput.SetText(value)
	input.recordEdit(input.GetText(), "")
	input.cursor, input.anchor = utf8.RuneCountInString(input.GetText()), -1
	input.dragging, input.wordDragging = false, false
	input.clickAt = time.Time{}
	input.caretAt = time.Now()
	input.editKind = ""
}

func (input *boundedInput) restoreEdit(redo bool) {
	input.recordEdit(input.GetText(), "")
	source, target := &input.undo, &input.redo
	if redo {
		source, target = target, source
	}
	if len(*source) == 0 {
		return
	}
	edit := (*source)[len(*source)-1]
	(*source)[len(*source)-1] = inputEdit{}
	*source = (*source)[:len(*source)-1]
	*target = pushInputEdit(*target, inputEdit{text: input.GetText(), cursor: input.cursor})
	input.TextInput.SetText(edit.text)
	length := utf8.RuneCountInString(input.GetText())
	input.cursor, input.anchor = max(0, min(edit.cursor, length)), -1
	input.dragging, input.wordDragging = false, false
	input.clickAt = time.Time{}
	input.caretAt = time.Now()
	input.editText, input.editKind = input.GetText(), ""
}

func (input *boundedInput) Update(update *widget.UpdateObject) {
	// Capture external changes before processing this frame's keyboard input.
	input.recordEdit(input.GetText(), "")
	if input.IsFocused() && !input.GetWidget().Disabled {
		mod := modifierDown("mod") && !modifierDown("alt")
		if mod && (ebiten.IsKeyPressed(ebiten.KeyZ) || ebiten.IsKeyPressed(ebiten.KeyY)) {
			if inpututil.IsKeyJustPressed(ebiten.KeyZ) {
				input.restoreEdit(modifierDown("shift"))
			} else if inpututil.IsKeyJustPressed(ebiten.KeyY) && !modifierDown("shift") {
				input.restoreEdit(true)
			}
			// Consume the chord, including on macOS where the dependency only
			// suppresses ordinary characters for Control, not Command.
			input.GetWidget().Update(update)
			return
		}
	}
	input.updateEditing()
	// Keep EbitenUI's focus, hover and layout events. Text/caret/selection are
	// managed together here because its TextInput has no public range setter.
	input.GetWidget().Update(update)
}

func (input *boundedInput) disposeBuffer() {
	if input != nil && input.buffer != nil {
		input.buffer.Dispose()
		input.buffer = nil
	}
}

func (input *boundedInput) Render(screen *ebiten.Image) {
	diagnostics.RecordInput(input.GetWidget().Rect.Dx(), input.GetWidget().Rect.Dy())
	if input.legacyDiagnostic {
		input.TextInput.Render(screen)
		return
	}
	rect := input.GetWidget().Rect
	clip := rect.Intersect(screen.Bounds())
	if clip.Empty() {
		return
	}
	local := image.Rect(0, 0, rect.Dx(), rect.Dy())
	if input.buffer == nil || input.buffer.Bounds() != local {
		if input.buffer != nil {
			input.buffer.Dispose()
		}
		input.buffer = ebiten.NewImageWithOptions(local, &ebiten.NewImageOptions{Unmanaged: true})
	}
	input.buffer.Clear()
	// Input processing and focus keep their original workspace coordinates.
	input.drawInput(input.buffer, local)
	options := &ebiten.DrawImageOptions{}
	options.GeoM.Translate(float64(rect.Min.X), float64(rect.Min.Y))
	screen.SubImage(clip).(*ebiten.Image).DrawImage(input.buffer, options)
}
