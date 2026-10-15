package ui

import (
	"image"
	"math"
	"strings"
	"unicode"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	extast "github.com/yuin/goldmark/extension/ast"
)

type nativeMarkdownFragment struct {
	nativeMarkdownSpan
	x, y, width, height float64
	face                text.Face
	clip                image.Rectangle
}

type nativeMarkdownDecoration struct {
	rect image.Rectangle
	kind string
}

type nativeMarkdownFace struct {
	size float64
	code bool
}

type nativeMarkdown struct {
	*widget.Container
	app           *app
	blocks        []nativeMarkdownBlock
	fragments     []nativeMarkdownFragment
	decorations   []nativeMarkdownDecoration
	anchors       map[string]float64
	faces         map[nativeMarkdownFace]text.Face
	width, height int
	viewport      func() image.Rectangle
}

func (v *nativeMarkdown) PreferredSize() (int, int) {
	width := max(v.app.px(100), min(v.app.px(560), v.app.width-v.app.px(128)))
	if v.width == width {
		return width, v.height
	}
	v.width = width
	v.fragments = nil
	v.decorations = nil
	v.anchors = make(map[string]float64)
	v.faces = make(map[nativeMarkdownFace]text.Face)
	pad := float64(v.app.px(8))
	y := pad
	for _, block := range v.blocks {
		left := pad + math.Min(float64(v.app.px(block.indent)), float64(width)/2)
		right := math.Max(left+float64(v.app.px(20)), float64(width)-pad)
		start := y
		if block.anchor != "" {
			v.anchors[block.anchor] = y
		}
		if block.rule {
			v.decorate(left, y+pad/2, right, y+pad/2+float64(v.app.px(1)), "line")
			y += 2 * pad
			continue
		}
		if block.table != nil {
			y = v.layoutTable(block.table, left, right, y)
		} else {
			if block.code || block.notice {
				left += pad
				right -= pad
				y += pad
			}
			if block.marker != "" {
				face := v.face(nativeMarkdownStyle{})
				width, _ := text.Measure(block.marker, face, 0)
				v.fragments = append(v.fragments, nativeMarkdownFragment{nativeMarkdownSpan: nativeMarkdownSpan{value: block.marker}, x: left - width, y: y, width: width, height: float64(v.app.px(22)), face: face})
			}
			y = v.layoutSpans(block.spans, left, right, y, block.code)
			if block.code || block.notice {
				y += pad
				v.decorate(left-pad, start, right+pad, y, "background")
			}
		}
		if block.quote || block.notice {
			v.decorate(left-pad, start, left-pad+float64(v.app.px(3)), y, "accent")
		}
		y += pad
	}
	v.height = int(math.Ceil(y + pad))
	return width, v.height
}

func (v *nativeMarkdown) face(style nativeMarkdownStyle) text.Face {
	size := style.size
	if size == 0 {
		size = 14
	}
	key := nativeMarkdownFace{size: size, code: style.code}
	if face := v.faces[key]; face != nil {
		return face
	}
	source := v.app.fontSource
	if style.code {
		source = v.app.monoSource
	}
	face, _ := text.NewMultiFace(&text.GoTextFace{Source: source, Size: size * v.app.displayScale}, &text.GoTextFace{Source: v.app.fontSource, Size: size * v.app.displayScale})
	v.faces[key] = face
	return face
}

func (v *nativeMarkdown) decorate(left, top, right, bottom float64, kind string) {
	v.decorations = append(v.decorations, nativeMarkdownDecoration{rect: image.Rect(int(math.Round(left)), int(math.Round(top)), int(math.Round(right)), int(math.Round(bottom))), kind: kind})
}

func (v *nativeMarkdown) layoutSpans(spans []nativeMarkdownSpan, left, right, y float64, code bool) float64 {
	x := left
	lineHeight := float64(v.app.px(22))
	start := len(v.fragments)
	newLine := func() { y += lineHeight; x = left; lineHeight = float64(v.app.px(22)) }
	for _, span := range spans {
		face := v.face(span.style)
		_, height := text.Measure("Mg", face, 0)
		height = math.Max(height, float64(v.app.px(22)))
		place := func(value string) {
			width, _ := text.Measure(value, face, 0)
			if x > left && x+width > right {
				newLine()
			}
			lineHeight = math.Max(lineHeight, height+float64(v.app.px(2)))
			if count := len(v.fragments); count > start {
				previous := &v.fragments[count-1]
				if previous.y == y && previous.style == span.style && math.Abs(previous.x+previous.width-x) < .01 {
					combined := previous.value + value
					combinedWidth, _ := text.Measure(combined, face, 0)
					if previous.x+combinedWidth <= right {
						previous.value = combined
						previous.width = combinedWidth
						previous.height = lineHeight
						x = previous.x + combinedWidth
						return
					}
				}
			}
			v.fragments = append(v.fragments, nativeMarkdownFragment{nativeMarkdownSpan: nativeMarkdownSpan{value: value, style: span.style}, x: x, y: y, width: width, height: lineHeight, face: face})
			x += width
		}
		var word strings.Builder
		flush := func() {
			value := word.String()
			word.Reset()
			width, _ := text.Measure(value, face, 0)
			if width <= right-left {
				place(value)
			} else {
				for _, r := range value {
					place(string(r))
				}
			}
		}
		for _, r := range strings.ReplaceAll(span.value, "\t", "    ") {
			if r == '\n' {
				if word.Len() > 0 {
					flush()
				}
				newLine()
				continue
			}
			if unicode.IsSpace(r) || unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
				if word.Len() > 0 {
					flush()
				}
				if x > left || !unicode.IsSpace(r) || code {
					place(string(r))
				}
			} else {
				word.WriteRune(r)
			}
		}
		if word.Len() > 0 {
			flush()
		}
	}
	return y + lineHeight
}

func (v *nativeMarkdown) layoutTable(table *nativeMarkdownTable, left, right, y float64) float64 {
	count := len(table.alignments)
	if count == 0 || len(table.rows) == 0 {
		return y
	}
	pad := float64(v.app.px(6))
	available := right - left
	minimum := math.Min(float64(v.app.px(72)), available/float64(count))
	widths := make([]float64, count)
	weight := 0.0
	for _, row := range table.rows {
		for column, cell := range row {
			if column >= count {
				break
			}
			width := 2 * pad
			for _, span := range cell {
				part, _ := text.Measure(span.value, v.face(span.style), 0)
				width += part
			}
			widths[column] = math.Max(widths[column], math.Min(width, float64(v.app.px(400))))
		}
	}
	for column := range widths {
		widths[column] = math.Max(0, widths[column]-minimum)
		weight += widths[column]
	}
	extra := math.Max(0, available-minimum*float64(count))
	for column := range widths {
		if weight > 0 {
			widths[column] = minimum + extra*widths[column]/weight
		} else {
			widths[column] = available / float64(count)
		}
	}
	for rowIndex, row := range table.rows {
		x := left
		bottom := y
		start := len(v.fragments)
		for column := 0; column < count; column++ {
			var cell []nativeMarkdownSpan
			if column < len(row) {
				cell = row[column]
			}
			cellPad := math.Min(pad, widths[column]/4)
			cellLeft, cellRight := x+cellPad, x+widths[column]-cellPad
			first := len(v.fragments)
			end := v.layoutSpans(cell, cellLeft, cellRight, y+pad, false)
			bottom = math.Max(bottom, end+pad)
			// Align each wrapped line independently, keeping links in their
			// final positions rather than moving only the painted text.
			for line := first; line < len(v.fragments); {
				last := line + 1
				edge := v.fragments[line].x + v.fragments[line].width
				for last < len(v.fragments) && v.fragments[last].y == v.fragments[line].y {
					edge = math.Max(edge, v.fragments[last].x+v.fragments[last].width)
					last++
				}
				offset := 0.0
				if table.alignments[column] == extast.AlignRight {
					offset = math.Max(0, cellRight-edge)
				} else if table.alignments[column] == extast.AlignCenter {
					offset = math.Max(0, (cellRight-edge)/2)
				}
				for i := line; i < last; i++ {
					v.fragments[i].x += offset
					v.fragments[i].clip = image.Rect(int(math.Ceil(cellLeft)), int(y), int(math.Floor(cellRight)), 0)
				}
				line = last
			}
			x += widths[column]
		}
		for i := start; i < len(v.fragments); i++ {
			v.fragments[i].clip.Max.Y = int(math.Ceil(bottom))
		}
		if rowIndex == 0 {
			v.decorate(left, y, right, bottom, "background")
		}
		x = left
		for _, width := range widths {
			v.decorate(x, y, x+width, bottom, "border")
			x += width
		}
		y = bottom
	}
	return y
}

func (v *nativeMarkdown) linkAt(position image.Point) string {
	if v.viewport != nil && !position.In(v.viewport()) {
		return ""
	}
	point := position.Sub(v.GetWidget().Rect.Min)
	for _, part := range v.fragments {
		if part.style.link == "" || !part.clip.Empty() && !point.In(part.clip) {
			continue
		}
		if float64(point.X) >= part.x && float64(point.X) < part.x+part.width && float64(point.Y) >= part.y && float64(point.Y) < part.y+part.height {
			return part.style.link
		}
	}
	return ""
}

func (v *nativeMarkdown) Render(screen *ebiten.Image) {
	v.Container.Render(screen)
	if !v.GetWidget().IsVisible() {
		return
	}
	v.PreferredSize()
	origin := v.GetWidget().Rect.Min
	clip := screen.Bounds().Intersect(v.GetWidget().Rect)
	if v.viewport != nil {
		clip = clip.Intersect(v.viewport())
	}
	if clip.Empty() {
		return
	}
	screen = screen.SubImage(clip).(*ebiten.Image)
	colors := v.app.colors()
	for _, item := range v.decorations {
		rect := item.rect.Add(origin)
		if !rect.Overlaps(clip) {
			continue
		}
		color := colors.elevated
		if item.kind == "accent" {
			color = colors.accent
		} else if item.kind == "line" || item.kind == "border" {
			color = colors.line
		}
		if item.kind == "border" {
			vector.StrokeRect(screen, float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy()), float32(v.app.px(1)), color, false)
		} else {
			vector.DrawFilledRect(screen, float32(rect.Min.X), float32(rect.Min.Y), float32(rect.Dx()), float32(rect.Dy()), color, false)
		}
	}
	for _, part := range v.fragments {
		x, y := float64(origin.X)+part.x, float64(origin.Y)+part.y
		if y+part.height < float64(clip.Min.Y) || y > float64(clip.Max.Y) {
			continue
		}
		target := screen
		if !part.clip.Empty() {
			cellClip := clip.Intersect(part.clip.Add(origin))
			if cellClip.Empty() {
				continue
			}
			target = screen.SubImage(cellClip).(*ebiten.Image)
		}
		if part.style.code {
			vector.DrawFilledRect(target, float32(x), float32(y), float32(part.width), float32(part.height), colors.elevated, false)
		}
		color := colors.text
		if part.style.link != "" {
			color = colors.accent
		}
		op := &text.DrawOptions{}
		op.ColorScale.ScaleWithColor(color)
		if part.style.italic {
			op.GeoM.Skew(-.15, 0)
			x += part.height * .15
		}
		op.GeoM.Translate(x, y)
		text.Draw(target, part.value, part.face, op)
		if part.style.bold {
			op.GeoM.Translate(.6*v.app.displayScale, 0)
			text.Draw(target, part.value, part.face, op)
		}
		if part.style.link != "" || part.style.strike {
			lineY := y + part.height - float64(v.app.px(4))
			if part.style.strike {
				lineY = y + part.height/2
			}
			vector.DrawFilledRect(target, float32(x), float32(lineY), float32(part.width), float32(v.app.px(1)), color, false)
		}
	}
}
