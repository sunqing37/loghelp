package ui

import (
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

func (w *app) lightTheme() bool {
	return w.config.Theme == "light" || w.config.Theme == "system" && w.systemColorMode != ebiten.ColorModeDark
}

// Ebitengine reads Windows application appearance, macOS appearance, and Linux
// GSettings/GTK preferences. Query off the UI thread because Linux can invoke
// gsettings. An unknown preference uses the light palette.
func (w *app) monitorSystemTheme(lastMode ebiten.ColorMode) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			mode := ebiten.SystemColorMode()
			if mode != lastMode {
				lastMode = mode
				w.post(func() { w.detectedSystemColorMode = mode })
			}
		}
	}
}

func (w *app) applySystemTheme() {
	if w.systemColorMode == w.detectedSystemColorMode {
		return
	}
	if w.config.Theme != "system" {
		w.systemColorMode = w.detectedSystemColorMode
		return
	}
	// Preserve dialogs, their unsaved drafts, active editors and drag gestures;
	// apply the latest appearance once these interactions finish.
	if w.modal || w.fileDialogOpen || len(inpututil.AppendPressedKeys(nil)) > 0 || ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) ||
		w.columnDrag.key != "" || w.detailDragging || w.tabDrag.page != nil {
		return
	}
	inputs := w.workspaceInputs()
	focused := -1
	for i, input := range inputs {
		if input != nil && input.IsFocused() {
			if time.Since(input.editAt) < time.Second {
				return
			}
			focused = i
		}
	}
	if w.ui.HasFocus() && focused < 0 {
		return
	}
	w.systemColorMode = w.detectedSystemColorMode
	w.rebuild()
	if focused >= 0 {
		w.ui.SetFocusedWidget(w.workspaceInputs()[focused])
	}
	w.restoreWorkspaceInputs(inputs)
	w.renderDirty = true
}
