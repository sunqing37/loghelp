package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// compactText preserves its full Label for hints and dialogs while rendering
// a single measured line. Long paths and task messages cannot grow a toolbar.
type compactText struct {
	*widget.Text
	face *text.Face
}

func (w *app) oneLine(value string, small bool, width int) *compactText {
	face, foreground := &w.uiFace, w.colors().text
	if small {
		face, foreground = &w.smallFace, w.colors().muted
	}
	label := &compactText{Text: w.textLabel(value, face, foreground), face: face}
	label.MaxWidth = float64(w.px(width))
	w.attachHint(label.GetWidget(), func() string { return label.Label })
	return label
}

func (label *compactText) PreferredSize() (int, int) {
	width, height := text.Measure(label.Label, *label.face, 0)
	if label.MaxWidth > 0 {
		width = math.Min(width, label.MaxWidth)
	}
	return max(int(math.Ceil(width)), label.GetWidget().MinWidth), max(int(math.Ceil(height)), label.GetWidget().MinHeight)
}

func (label *compactText) Render(screen *ebiten.Image) {
	clip := label.GetWidget().Rect.Intersect(screen.Bounds())
	if !label.GetWidget().IsVisible() || clip.Empty() {
		return
	}
	full, limit := label.Label, label.MaxWidth
	label.Label = fitFace(full, float64(max(label.GetWidget().Rect.Dx(), 0)), *label.face)
	label.MaxWidth = 0
	label.Text.Render(screen.SubImage(clip).(*ebiten.Image))
	label.Label, label.MaxWidth = full, limit
}

func (w *app) attachHint(target *widget.Widget, value func() string) {
	label := w.muted(value())
	label.MaxWidth = float64(w.px(440))
	target.ToolTips = append(target.ToolTips, widget.NewToolTip(
		widget.ToolTipOpts.Content(w.card(label, 10)),
		widget.ToolTipOpts.Delay(600*time.Millisecond),
		widget.ToolTipOpts.Position(widget.TOOLTIP_POS_WIDGET),
		widget.ToolTipOpts.ToolTipUpdater(func(widget.Containerer) { label.Label = value() }),
	))
}

func (w *app) hintAction(label, hint, kind string, action func()) *widget.Button {
	button := w.action(label, kind, action)
	w.attachHint(button.GetWidget(), func() string { return hint })
	return button
}

func (w *app) sectionTitle(value string) *widget.Text {
	return w.textLabel(value, &w.sectionFace, w.colors().text)
}

func (w *app) workspaceWidth() int {
	width := w.width - w.px(224+28)
	if w.preview != nil {
		width -= w.px(320)
	}
	return max(width, w.px(200))
}

// The same width calculation is used during measurement and placement, so a
// narrow window moves actions to a second row without rebuilding input fields.
type toolbarLayout struct {
	width func() int
	gap   int
}

func (layout *toolbarLayout) PreferredSize(children []widget.PreferredSizeLocateableWidget) (int, int) {
	if len(children) != 2 {
		return 0, 0
	}
	lw, lh := children[0].PreferredSize()
	rw, rh := children[1].PreferredSize()
	if lw+rw+layout.gap > layout.width() {
		return min(max(lw, rw), layout.width()), lh + rh + layout.gap
	}
	return lw + rw + layout.gap, max(lh, rh)
}

func (layout *toolbarLayout) Layout(children []widget.PreferredSizeLocateableWidget, rect image.Rectangle) {
	if len(children) != 2 {
		return
	}
	lw, lh := children[0].PreferredSize()
	rw, rh := children[1].PreferredSize()
	if lw+rw+layout.gap > rect.Dx() {
		children[0].SetLocation(image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+lh))
		children[1].SetLocation(image.Rect(max(rect.Min.X, rect.Max.X-rw), rect.Min.Y+lh+layout.gap, rect.Max.X, rect.Min.Y+lh+layout.gap+rh))
		return
	}
	children[0].SetLocation(image.Rect(rect.Min.X, rect.Min.Y+(rect.Dy()-lh)/2, rect.Max.X-rw-layout.gap, rect.Min.Y+(rect.Dy()-lh)/2+lh))
	children[1].SetLocation(image.Rect(rect.Max.X-rw, rect.Min.Y+(rect.Dy()-rh)/2, rect.Max.X, rect.Min.Y+(rect.Dy()-rh)/2+rh))
}

func (w *app) toolbar(left, right widget.PreferredSizeLocateableWidget, width func() int) *widget.Container {
	box := widget.NewContainer(widget.ContainerOpts.Layout(&toolbarLayout{width: width, gap: w.px(8)}))
	box.AddChild(left, right)
	return box
}

// Save layout facts alongside the existing app-owned screenshot, without
// reading the desktop, injecting input, or changing the user's configuration.
func (w *app) saveLayoutSnapshot() {
	rects := map[string]image.Rectangle{}
	add := func(name string, target widget.HasWidget) { rects[name] = target.GetWidget().Rect }
	if w.logPanel != nil {
		add("logs", w.logPanel)
	}
	if w.rowHeader != nil {
		add("table_header", w.rowHeader)
	}
	if w.detailPanel != nil {
		add("details", w.detailPanel)
	}
	if w.filterInput != nil {
		add("filter", w.filterInput)
	}
	if w.searchInput != nil {
		add("search", w.searchInput)
	}
	if w.serialInput != nil {
		add("device_input", w.serialInput)
	}
	if w.buffersInput != nil {
		add("buffers", w.buffersInput)
	}
	if w.statusLabel != nil {
		add("status", w.statusLabel)
	}
	if w.copyJSONButton != nil {
		add("copy_json", w.copyJSONButton)
	}
	if w.cancelJobButton != nil {
		add("cancel_job", w.cancelJobButton)
	}
	data := struct {
		Width, Height          int
		Scale                  float64
		WindowVisible          bool
		RowHeight, VisibleRows int
		Rects                  map[string]image.Rectangle
	}{w.width, w.height, w.displayScale, ebiten.IsWindowVisible(), w.rowHeight(), w.visibleRows(), rects}
	content, err := json.MarshalIndent(data, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(os.Getenv("LOGHELP_PROFILE_DIR"), "layout.json"), content, 0600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile layout:", err)
	}
}
