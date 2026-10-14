package ui

import (
	"fmt"
	"os"
	"unicode/utf8"
)

// Seed a selection for a manual background preview without reading user input
// or touching the system clipboard. Requires the existing diagnostic mode.
func (w *app) applyProfileSelection(p *page) {
	value := os.Getenv("LOGHELP_PROFILE_SELECTION")
	if !w.backgroundDiagnostic || value == "" || w.profileSelectionApplied || p.busy || !w.prepareDetails(p) {
		return
	}
	raw := p.detailCache.entry.Raw
	start, end := 0, len(raw)
	if value != "all" {
		if count, err := fmt.Sscanf(value, "%d:%d", &start, &end); err != nil || count != 2 {
			return
		}
	}
	if start < 0 || end > len(raw) || start >= end || !utf8.RuneStart(raw[start]) || end < len(raw) && !utf8.RuneStart(raw[end]) {
		return
	}
	p.detailSelection = detailSelection{anchor: start, caret: end, focused: true}
	w.profileSelectionApplied = true
	w.renderDirty = true
}
