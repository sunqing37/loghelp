package ui

import (
	"image"
	"image/color"
	"path"
	"strings"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"loghelp/internal/core"
)

type deviceFileRow struct {
	file   core.DeviceFile
	path   string
	depth  int
	notice string
}

// Keep the full hierarchy in memory, but draw only the visible viewport. Large
// directories no longer create thousands of buttons or split trees into pages.
func (s *deviceFileExplorer) fileRows() []deviceFileRow {
	var rows []deviceFileRow
	filter := strings.ToLower(s.filter)
	var appendRows func(string, []core.DeviceFile, int)
	appendRows = func(parent string, entries []core.DeviceFile, depth int) {
		if depth > 32 {
			return
		}
		for _, file := range entries {
			remote := path.Join(parent, file.Name)
			if filter == "" || strings.Contains(strings.ToLower(file.Name), filter) {
				rows = append(rows, deviceFileRow{file: file, path: remote, depth: depth})
			}
			if s.expanded[remote] || filter != "" {
				if err := s.directoryErrors[remote]; err != nil {
					if filter == "" || strings.Contains(strings.ToLower(file.Name), filter) {
						rows = append(rows, deviceFileRow{path: remote, depth: depth + 1, notice: deviceDirectoryNotice(err)})
					}
				} else {
					appendRows(remote, s.children[remote], depth+1)
				}
			}
		}
	}
	if err := s.directoryErrors[s.directory]; err != nil {
		return []deviceFileRow{{path: s.directory, notice: deviceDirectoryNotice(err)}}
	}
	appendRows(s.directory, s.entries, 0)
	return rows
}

type deviceFileTable struct {
	*widget.Button
	app      *app
	state    *deviceFileExplorer
	rows     []deviceFileRow
	dragging bool
}

func (w *app) newDeviceFileTable(s *deviceFileExplorer) *deviceFileTable {
	v := &deviceFileTable{app: w, state: s}
	images, _ := w.buttonStyle("quiet")
	v.Button = widget.NewButton(widget.ButtonOpts.Image(images),
		widget.ButtonOpts.WidgetOpts(widget.WidgetOpts.MinSize(w.px(320), w.px(120)),
			widget.WidgetOpts.ScrolledHandler(func(e *widget.WidgetScrolledEventArgs) {
				if !s.visible || w.modal || w.fileDialogOpen {
					return
				}
				s.offset -= int(e.Y * 3)
				v.clampScroll()
				w.renderDirty = true
			})),
		widget.ButtonOpts.ClickedHandler(func(e *widget.ButtonClickedEventArgs) {
			if s.busy || !s.visible || w.modal || w.fileDialogOpen {
				return
			}
			if e.OffsetX >= v.GetWidget().Rect.Dx()-w.px(14) {
				v.scrollAt(e.OffsetY)
				return
			}
			i := v.rowAt(e.OffsetY)
			if i < 0 || i >= len(v.rows) {
				return
			}
			row := v.rows[i]
			if row.notice != "" {
				return
			}
			double := s.selected == row.path && time.Since(s.clicked) < 500*time.Millisecond
			s.selected, s.clicked = row.path, time.Now()
			s.refresh()
			w.ui.SetFocusedWidget(v)
			if row.file.IsDir() || row.file.IsLink() {
				indent := w.px(10 + min(row.depth, 8)*16)
				if e.OffsetX >= indent && e.OffsetX < indent+w.px(18) {
					w.toggleDeviceDirectory(row.path)
				} else if double {
					w.loadDeviceDirectory(row.path)
				}
			} else if double && row.file.IsRegular() {
				w.downloadDeviceFile()
			}
		}))
	v.DisableDefaultKeys = true
	w.attachHint(v.GetWidget(), func() string {
		x, y := ebiten.CursorPosition()
		if !image.Pt(x, y).In(v.GetWidget().Rect) {
			return ""
		}
		i := v.rowAt(y - v.GetWidget().Rect.Min.Y)
		if i >= 0 && i < len(v.rows) {
			row := v.rows[i]
			if err := s.directoryErrors[row.path]; err != nil {
				return row.path + "\n" + deviceDirectoryNotice(err)
			}
			return row.path + "\n" + deviceFilePermissions(row.file) + " · " + row.file.Modified.Format("2006-01-02 15:04")
		}
		return "↑ ↓ 选择 · → 展开 · ← 折叠 · Enter 打开目录或下载文件"
	})
	return v
}

func (v *deviceFileTable) visibleCount() int {
	return max(1, (v.GetWidget().Rect.Dy()-v.app.px(34))/v.app.px(28))
}
func (v *deviceFileTable) clampScroll() {
	v.state.offset = max(0, min(v.state.offset, max(0, len(v.rows)-v.visibleCount())))
}
func (v *deviceFileTable) rowAt(y int) int {
	if y < v.app.px(34) {
		return -1
	}
	return v.state.offset + (y-v.app.px(34))/v.app.px(28)
}
func (v *deviceFileTable) scrollAt(y int) {
	height := max(1, v.GetWidget().Rect.Dy()-v.app.px(34))
	v.state.offset = int(float64(y-v.app.px(34)) / float64(height) * float64(len(v.rows)))
	v.clampScroll()
	v.app.renderDirty = true
}
func (v *deviceFileTable) Update(update *widget.UpdateObject) {
	v.Button.Update(update)
	w, s := v.app, v.state
	if !s.visible || s.table != v || w.modal || w.fileDialogOpen {
		v.dragging = false
		return
	}
	x, y := ebiten.CursorPosition()
	r := v.GetWidget().Rect
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) && image.Pt(x, y).In(r) && x >= r.Max.X-w.px(14) {
		v.dragging = true
	}
	if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		v.dragging = false
	}
	if v.dragging {
		v.scrollAt(y - r.Min.Y)
	}
	if s.busy || !v.IsFocused() || len(v.rows) == 0 {
		return
	}
	i := -1
	for j, row := range v.rows {
		if row.notice == "" && row.path == s.selected {
			i = j
			break
		}
	}
	next := i
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowDown):
		next = min(len(v.rows)-1, i+1)
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowUp):
		next = max(0, i-1)
	case inpututil.IsKeyJustPressed(ebiten.KeyHome):
		next = 0
	case inpututil.IsKeyJustPressed(ebiten.KeyEnd):
		next = len(v.rows) - 1
	case inpututil.IsKeyJustPressed(ebiten.KeyPageDown):
		next = min(len(v.rows)-1, max(0, i)+v.visibleCount())
	case inpututil.IsKeyJustPressed(ebiten.KeyPageUp):
		next = max(0, i-v.visibleCount())
	case i >= 0 && inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		if v.rows[i].file.IsDir() || v.rows[i].file.IsLink() {
			w.loadDeviceDirectory(s.selected)
		} else if v.rows[i].file.IsRegular() {
			w.downloadDeviceFile()
		}
		return
	case i >= 0 && inpututil.IsKeyJustPressed(ebiten.KeyArrowRight):
		if (v.rows[i].file.IsDir() || v.rows[i].file.IsLink()) && !s.expanded[s.selected] {
			w.toggleDeviceDirectory(s.selected)
		}
		return
	case i >= 0 && inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft):
		if s.expanded[s.selected] {
			w.toggleDeviceDirectory(s.selected)
		} else {
			parent := path.Dir(s.selected)
			for j, row := range v.rows {
				if row.path == parent {
					next = j
					break
				}
			}
		}
	}
	if next >= 0 && next != i {
		step := 1
		if next < i || inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
			step = -1
		}
		for next >= 0 && next < len(v.rows) && v.rows[next].notice != "" {
			next += step
		}
		if next < 0 || next >= len(v.rows) {
			return
		}
		s.selected = v.rows[next].path
		if next < s.offset {
			s.offset = next
		}
		if next >= s.offset+v.visibleCount() {
			s.offset = next - v.visibleCount() + 1
		}
		s.refresh()
	}
}

func deviceFilePermissions(file core.DeviceFile) string {
	value := []byte("----------")
	if file.IsDir() {
		value[0] = 'd'
	} else if file.IsLink() {
		value[0] = 'l'
	}
	for i, c := range "rwxrwxrwx" {
		if file.Mode&(1<<uint(8-i)) != 0 {
			value[i+1] = byte(c)
		}
	}
	for _, special := range []struct {
		mask    uint32
		index   int
		on, off byte
	}{{04000, 3, 's', 'S'}, {02000, 6, 's', 'S'}, {01000, 9, 't', 'T'}} {
		if file.Mode&special.mask != 0 {
			if value[special.index] == 'x' {
				value[special.index] = special.on
			} else {
				value[special.index] = special.off
			}
		}
	}
	return string(value)
}

func (v *deviceFileTable) Render(screen *ebiten.Image) {
	w, s := v.app, v.state
	r := v.GetWidget().Rect.Intersect(screen.Bounds())
	if r.Empty() || !v.GetWidget().IsVisible() {
		return
	}
	v.GetWidget().Render(screen)
	v.clampScroll()
	canvas := screen.SubImage(r).(*ebiten.Image)
	c := w.colors()
	canvas.Fill(c.bg)
	header, rowHeight := w.px(34), w.px(28)
	// Preserve file-name space in the dock. Full metadata remains available in
	// the selection details and tooltip, and becomes columns in wider layouts.
	dateWidth, permissionsWidth, sizeWidth := 0, 0, w.px(88)
	if r.Dx() >= w.px(720) {
		dateWidth, permissionsWidth = w.px(156), w.px(110)
	}
	nameEnd := r.Max.X - w.px(16) - dateWidth - permissionsWidth - sizeWidth
	permissionEnd, dateEnd := nameEnd+permissionsWidth, nameEnd+permissionsWidth+dateWidth
	vector.FillRect(canvas, float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(header), c.surface, false)
	for i, label := range []string{"名称", "权限", "修改时间", "大小"} {
		if (i == 1 && permissionsWidth == 0) || (i == 2 && dateWidth == 0) {
			continue
		}
		x := []int{r.Min.X + w.px(12), nameEnd + w.px(8), permissionEnd + w.px(8), dateEnd + w.px(8)}[i]
		w.paintFace(canvas, label, float64(x), float64(r.Min.Y+w.px(8)), w.smallFace, c.muted)
	}
	vector.FillRect(canvas, float32(r.Min.X), float32(r.Min.Y+header-1), float32(r.Dx()), 1, c.line, false)
	mx, my := ebiten.CursorPosition()
	hover := -1
	if image.Pt(mx, my).In(r) {
		hover = v.rowAt(my - r.Min.Y)
	}
	_, fontHeight := text.Measure("Ag", w.uiFace, 0)
	for i := s.offset; i < min(len(v.rows), s.offset+v.visibleCount()+1); i++ {
		row := v.rows[i]
		y := r.Min.Y + header + (i-s.offset)*rowHeight
		if row.notice != "" {
			x := r.Min.X + w.px(10+min(row.depth, 8)*16)
			vector.FillRect(canvas, float32(r.Min.X+1), float32(y), float32(r.Dx()-2), float32(rowHeight), c.dangerSoft, false)
			message := fitFace("!  "+row.notice, float64(max(0, r.Max.X-x-w.px(16))), w.smallFace)
			_, height := text.Measure("Ag", w.smallFace, 0)
			w.paintFace(canvas, message, float64(x), float64(y)+(float64(rowHeight)-height)/2, w.smallFace, c.danger)
			continue
		}
		if row.path == s.selected {
			vector.FillRect(canvas, float32(r.Min.X+1), float32(y), float32(r.Dx()-2), float32(rowHeight), c.selection, false)
			vector.FillRect(canvas, float32(r.Min.X+1), float32(y), float32(w.px(2)), float32(rowHeight), c.accent, false)
		} else if i == hover && !s.busy {
			vector.FillRect(canvas, float32(r.Min.X+1), float32(y), float32(r.Dx()-2), float32(rowHeight), c.elevated, false)
		}
		x := r.Min.X + w.px(10+min(row.depth, 8)*16)
		cy := float32(y + rowHeight/2)
		expanded := s.expanded[row.path] || (s.filter != "" && len(s.children[row.path]) > 0)
		fillShape := func(points []image.Point, shade color.NRGBA) {
			var shape vector.Path
			for i, point := range points {
				if i == 0 {
					shape.MoveTo(float32(point.X), float32(point.Y))
				} else {
					shape.LineTo(float32(point.X), float32(point.Y))
				}
			}
			shape.Close()
			options := &vector.DrawPathOptions{AntiAlias: true}
			options.ColorScale.ScaleWithColor(shade)
			vector.FillPath(canvas, &shape, nil, options)
		}
		if row.file.IsDir() || row.file.IsLink() {
			ax, ay := x+w.px(7), int(cy)
			shade := c.text
			if expanded {
				shade = c.accent
				fillShape([]image.Point{image.Pt(ax-w.px(5), ay-w.px(3)), image.Pt(ax+w.px(5), ay-w.px(3)), image.Pt(ax, ay+w.px(4))}, shade)
			} else {
				fillShape([]image.Point{image.Pt(ax-w.px(3), ay-w.px(5)), image.Pt(ax-w.px(3), ay+w.px(5)), image.Pt(ax+w.px(4), ay)}, shade)
			}
		}
		ix, iy := x+w.px(20), int(cy)-w.px(6)
		if row.file.IsDir() || row.file.IsLink() {
			// Closed folders have a solid front; open folders have a slanted flap.
			vector.StrokeRect(canvas, float32(ix), float32(iy), float32(w.px(14)), float32(w.px(12)), float32(w.px(1)), c.accent, true)
			vector.FillRect(canvas, float32(ix), float32(iy-w.px(2)), float32(w.px(6)), float32(w.px(3)), c.accent, false)
			if expanded {
				fillShape([]image.Point{image.Pt(ix+w.px(3), iy+w.px(4)), image.Pt(ix+w.px(18), iy+w.px(4)), image.Pt(ix+w.px(14), iy+w.px(12)), image.Pt(ix, iy+w.px(12))}, c.accent)
			} else {
				vector.FillRect(canvas, float32(ix), float32(iy+w.px(3)), float32(w.px(14)), float32(w.px(9)), c.accent, false)
			}
		} else {
			vector.StrokeRect(canvas, float32(ix+w.px(2)), float32(iy), float32(w.px(10)), float32(w.px(13)), float32(w.px(1)), c.muted, true)
			vector.StrokeLine(canvas, float32(ix+w.px(4)), float32(iy+w.px(5)), float32(ix+w.px(10)), float32(iy+w.px(5)), float32(w.px(1)), c.muted, true)
		}
		textY := float64(y) + (float64(rowHeight)-fontHeight)/2
		nameX := x + w.px(42)
		name := strings.NewReplacer("\n", "↵", "\r", "↵", "\t", " ").Replace(row.file.Name)
		w.paintFace(canvas, fitFace(name, float64(max(0, nameEnd-nameX-w.px(8))), w.uiFace), float64(nameX), textY, w.uiFace, c.text)
		if permissionsWidth > 0 {
			w.paintFace(canvas, deviceFilePermissions(row.file), float64(nameEnd+w.px(8)), textY, w.smallFace, c.muted)
		}
		if dateWidth > 0 {
			w.paintFace(canvas, row.file.Modified.Format("2006-01-02 15:04"), float64(permissionEnd+w.px(8)), textY, w.smallFace, c.muted)
		}
		size := "—"
		if row.file.IsRegular() {
			size = deviceFileSize(row.file.Size)
		}
		sizeMeasure, _ := text.Measure(size, w.smallFace, 0)
		w.paintFace(canvas, size, float64(r.Max.X-w.px(24))-sizeMeasure, textY, w.smallFace, c.muted)
	}
	if len(v.rows) == 0 {
		message := "此目录为空"
		if s.busy {
			message = "正在读取设备文件…"
		} else if !s.loaded {
			message = "选择在线设备，浏览设备上的文件"
		} else if s.filter != "" {
			message = "没有匹配的文件"
		}
		measure, _ := text.Measure(message, w.uiFace, 0)
		w.paintFace(canvas, message, float64(r.Min.X+r.Dx()/2)-measure/2, float64(r.Min.Y+header+(r.Dy()-header)/2), w.uiFace, c.muted)
	}
	if len(v.rows) > v.visibleCount() {
		track := r.Dy() - header
		thumb := max(w.px(24), track*v.visibleCount()/len(v.rows))
		y := r.Min.Y + header + (track-thumb)*s.offset/max(1, len(v.rows)-v.visibleCount())
		vector.FillRect(canvas, float32(r.Max.X-w.px(7)), float32(y), float32(w.px(4)), float32(thumb), c.controlBorder, false)
	}
	border := c.line
	if v.IsFocused() {
		border = c.accent
	}
	vector.StrokeRect(canvas, float32(r.Min.X)+0.5, float32(r.Min.Y)+0.5, float32(r.Dx()-1), float32(r.Dy()-1), 1, border, false)
}
