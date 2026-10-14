package ui

import (
	"image"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"loghelp/internal/diagnostics"
)

// boundedInput keeps EbitenUI's two masking images at the size of the input,
// rather than the entire workspace supplied to TextInput.Render.
type boundedInput struct {
	*widget.TextInput
	buffer           *ebiten.Image
	legacyDiagnostic bool
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
	// Input processing and focus use the original workspace coordinates.
	// Only the synchronous render runs at the small buffer's local origin.
	input.TextInput.SetLocation(local)
	input.TextInput.Render(input.buffer)
	input.TextInput.SetLocation(rect)
	options := &ebiten.DrawImageOptions{}
	options.GeoM.Translate(float64(rect.Min.X), float64(rect.Min.Y))
	screen.SubImage(clip).(*ebiten.Image).DrawImage(input.buffer, options)
}
