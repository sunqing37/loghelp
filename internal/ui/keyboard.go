package ui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"loghelp/internal/core"
)

var defaultShortcuts = map[string]string{"find": "Mod+F", "filter": "Mod+L", "new": "Mod+T", "close": "Mod+W", "next-tab": "Ctrl+Tab", "previous-tab": "Ctrl+Shift+Tab", "next-match": "F3", "previous-match": "Shift+F3", "pause": "Mod+Shift+P", "end": "Mod+End"}

func modifierDown(name string) bool {
	switch strings.ToLower(name) {
	case "mod":
		if runtime.GOOS == "darwin" {
			return ebiten.IsKeyPressed(ebiten.KeyMeta)
		}
		return ebiten.IsKeyPressed(ebiten.KeyControl)
	case "ctrl":
		return ebiten.IsKeyPressed(ebiten.KeyControl)
	case "cmd":
		return ebiten.IsKeyPressed(ebiten.KeyMeta)
	case "shift":
		return ebiten.IsKeyPressed(ebiten.KeyShift)
	case "alt":
		return ebiten.IsKeyPressed(ebiten.KeyAlt)
	}
	return false
}
func shortcutKey(value string) (ebiten.Key, error) {
	parts := strings.Split(value, "+")
	name := strings.TrimSpace(parts[len(parts)-1])
	var key ebiten.Key
	if err := key.UnmarshalText([]byte(name)); err != nil {
		return key, err
	}
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "mod", "ctrl", "cmd", "shift", "alt":
		default:
			return key, fmt.Errorf("无效修饰键 %s", part)
		}
	}
	if key == ebiten.KeyC || key == ebiten.KeyV {
		return key, fmt.Errorf("C/V 保留给复制粘贴")
	}
	return key, nil
}
func pressed(value string) bool {
	key, err := shortcutKey(value)
	if err != nil || !inpututil.IsKeyJustPressed(key) {
		return false
	}
	parts := strings.Split(value, "+")
	shiftExpected := false
	controlExpected := false
	altExpected := false
	for _, part := range parts[:len(parts)-1] {
		if !modifierDown(part) {
			return false
		}
		switch strings.ToLower(part) {
		case "shift":
			shiftExpected = true
		case "ctrl", "mod", "cmd":
			controlExpected = true
		case "alt":
			altExpected = true
		}
	}
	return shiftExpected == ebiten.IsKeyPressed(ebiten.KeyShift) && altExpected == ebiten.IsKeyPressed(ebiten.KeyAlt) && controlExpected == (ebiten.IsKeyPressed(ebiten.KeyControl) || ebiten.IsKeyPressed(ebiten.KeyMeta))
}
func (w *app) shortcut(name string) bool {
	value := defaultShortcuts[name]
	if custom := w.config.Shortcuts[name]; custom != "" {
		value = custom
	}
	return pressed(value)
}
func (w *app) keyboard() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		if w.tabDrag.page != nil {
			w.tabDrag.canceled = true
			return
		}
		if p := w.active(); p != nil && p.shell != nil && w.terminalFocus && !w.modal && !w.ui.HasFocus() {
			w.shellInput(p, "\x1b")
			return
		}
		if w.modal {
			w.closeModal()
		} else {
			w.ui.ClearFocus()
			w.terminalFocus = false
			if p := w.active(); p != nil {
				p.detailSelection = detailSelection{}
			}
		}
		return
	}
	if w.tabDrag.page != nil {
		return
	}
	if w.inputEditingKeys() {
		return
	}
	if w.modal {
		return
	}
	p := w.active()
	if p == nil {
		return
	}
	if w.shortcut("close") {
		w.closePage()
		return
	}
	if p.shell != nil && w.terminalFocus && !w.ui.HasFocus() {
		w.terminalKeys(p)
		return
	}
	if w.detailKeys(p) {
		return
	}
	if w.logKeys(p) {
		return
	}
	if w.shortcut("find") && p.shell == nil {
		w.ui.SetFocusedWidget(w.searchInput)
	}
	if w.shortcut("filter") && p.shell == nil {
		w.ui.SetFocusedWidget(w.filterInput)
	}
	if w.shortcut("new") {
		w.syncInputs()
		w.newPage(core.TabState{Name: "Logcat"})
		w.rebuild()
	}
	if w.shortcut("next-tab") {
		w.selectPage(w.selected + 1)
	}
	if w.shortcut("previous-tab") {
		w.selectPage(w.selected - 1)
	}
	if w.shortcut("next-match") {
		w.nextMatch(false)
	}
	if w.shortcut("previous-match") {
		w.nextMatch(true)
	}
	if w.shortcut("pause") {
		w.pause()
	}
	if w.shortcut("end") {
		p.keepSelectionVisible = false
		p.follow = true
		p.scroll = max(len(p.result.Rows)-w.visibleRows(), 0)
	}
	if !w.ui.HasFocus() && modifierDown("mod") && inpututil.IsKeyJustPressed(ebiten.KeyC) {
		w.copySelected()
	}
}
func (w *app) logKeys(p *page) bool {
	if p.shell != nil || w.ui.HasFocus() || w.detailDragging || w.columnDrag.key != "" || p.detailSelection.dragging || p.selected < 0 || p.selected >= len(p.result.Rows) {
		return false
	}
	if ebiten.IsKeyPressed(ebiten.KeyControl) || ebiten.IsKeyPressed(ebiten.KeyMeta) || ebiten.IsKeyPressed(ebiten.KeyAlt) || ebiten.IsKeyPressed(ebiten.KeyShift) {
		return false
	}
	delta := 0
	if w.logArrowPressed(ebiten.KeyArrowUp) {
		delta--
	}
	if w.logArrowPressed(ebiten.KeyArrowDown) {
		delta++
	}
	if delta == 0 {
		return false
	}
	p.keepSelectionVisible = false
	index := max(0, min(p.selected+delta, len(p.result.Rows)-1))
	if index == p.selected {
		return true
	}
	p.selectRow(index)
	p.detailSelection = detailSelection{}
	p.detailScroll, p.detailX = 0, 0
	p.follow = false
	visible := w.visibleRows()
	// Scroll only when the selection leaves the viewport.
	p.scroll = max(index-visible+1, min(p.scroll, index))
	p.scroll = max(0, min(p.scroll, max(len(p.result.Rows)-visible, 0)))
	w.renderDirty = true
	return true
}
func (w *app) logArrowPressed(key ebiten.Key) bool {
	if !keyRepeat(key) {
		return false
	}
	for name, value := range w.config.Shortcuts {
		if defaultShortcuts[name] == "" || strings.Contains(value, "+") {
			continue
		}
		if bound, err := shortcutKey(value); err == nil && bound == key {
			return false
		}
	}
	return true
}

// Editing chords take precedence over configurable workspace shortcuts only
// while an enabled text input owns focus. Undo/redo runs in boundedInput.Update.
func (w *app) inputEditingKeys() bool {
	focused := w.ui.GetFocusedWidget()
	if focused == nil || focused.GetWidget().Disabled || !modifierDown("mod") || modifierDown("alt") {
		return false
	}
	if _, ok := focused.(interface{ Insert(string) }); !ok {
		return false
	}
	if input, ok := focused.(interface{ IsFocused() bool }); !ok || !input.IsFocused() {
		return false
	}
	if ebiten.IsKeyPressed(ebiten.KeyZ) || ebiten.IsKeyPressed(ebiten.KeyY) {
		return true
	}
	for _, key := range []ebiten.Key{ebiten.KeyArrowLeft, ebiten.KeyArrowRight, ebiten.KeyHome, ebiten.KeyEnd} {
		if ebiten.IsKeyPressed(key) {
			return true
		}
	}
	if !modifierDown("shift") {
		for _, key := range []ebiten.Key{ebiten.KeyA, ebiten.KeyC, ebiten.KeyX, ebiten.KeyV} {
			if inpututil.IsKeyJustPressed(key) {
				w.pasteInput()
				return true
			}
		}
	}
	return false
}

func (w *app) pasteInput() {
	mod := ebiten.IsKeyPressed(ebiten.KeyControl)
	if runtime.GOOS == "darwin" {
		mod = ebiten.IsKeyPressed(ebiten.KeyMeta)
	}
	if !mod {
		return
	}
	widget := w.ui.GetFocusedWidget()
	if widget == nil {
		return
	}
	var focused any = widget
	if control, ok := widget.GetWidget().CustomData.(*boundedInput); ok {
		focused = control
	}
	if input, ok := focused.(interface {
		SelectAll()
		SelectedText() string
		DeleteSelectedText()
		Insert(string)
	}); ok {
		if inpututil.IsKeyJustPressed(ebiten.KeyA) {
			input.SelectAll()
			return
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyC) || inpututil.IsKeyJustPressed(ebiten.KeyX) {
			if value := input.SelectedText(); value != "" {
				if err := clipboard.WriteAll(value); err != nil {
					w.message("剪贴板", err.Error())
					return
				}
				if inpututil.IsKeyJustPressed(ebiten.KeyX) {
					input.DeleteSelectedText()
				}
			}
			return
		}
		if !inpututil.IsKeyJustPressed(ebiten.KeyV) {
			return
		}
		value, err := clipboard.ReadAll()
		if err != nil {
			w.message("剪贴板", err.Error())
			return
		}
		input.Insert(value)
	}
}
func (w *app) copySelected() {
	p := w.active()
	if p == nil || p.selected < 0 || p.selected >= len(p.result.Rows) {
		return
	}
	if err := clipboard.WriteAll(p.selectedRaw()); err != nil {
		w.message("剪贴板", err.Error())
	}
}
func (w *app) parseShortcuts(value string) (map[string]string, error) {
	custom := map[string]string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(value, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, key, ok := strings.Cut(line, "=")
		if !ok || defaultShortcuts[name] == "" {
			return nil, fmt.Errorf("无效绑定 %s", line)
		}
		if _, err := shortcutKey(key); err != nil {
			return nil, err
		}
		if seen[strings.ToLower(key)] {
			return nil, fmt.Errorf("快捷键重复：%s", key)
		}
		seen[strings.ToLower(key)] = true
		custom[name] = key
	}
	return custom, nil
}
