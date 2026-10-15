package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"loghelp/internal/core"
)

// markdownView keeps only visible image tiles on the GPU. Browser layout and
// PNG decoding run in the background; links retain their document coordinates.
type markdownView struct {
	*widget.Container
	app             *app
	plain           *widget.Text
	native          *nativeMarkdown
	document        *core.MarkdownDocument
	textures        map[int]*ebiten.Image
	loading         map[int]bool
	openLink        func(string)
	viewport        func() image.Rectangle
	scrollTo        func(float64)
	source, baseURL string
	generation      uint64
	cancel          context.CancelFunc
}

func (w *app) markdown(openLink func(string)) *markdownView {
	v := &markdownView{app: w, openLink: openLink}
	v.Container = widget.NewContainer(widget.ContainerOpts.Layout(widget.NewGridLayout(widget.GridLayoutOpts.Columns(1))), widget.ContainerOpts.WidgetOpts(
		widget.WidgetOpts.MouseButtonClickedHandler(func(a *widget.WidgetMouseButtonClickedEventArgs) {
			if a.Button != ebiten.MouseButtonLeft {
				return
			}
			link := v.linkAt(a.Widget.Rect.Min.Add(image.Pt(a.OffsetX, a.OffsetY)))
			if strings.HasPrefix(link, "#") {
				anchor, err := url.PathUnescape(strings.TrimPrefix(link, "#"))
				if err == nil {
					var y float64
					var ok bool
					if v.document != nil {
						y, ok = v.document.Anchors[anchor]
					} else if v.native != nil {
						y, ok = v.native.anchors[anchor]
					}
					if ok && v.scrollTo != nil {
						v.scrollTo(y)
					}
				}
				return
			}
			if strings.HasPrefix(link, "details:") && v.document != nil {
				index, err := strconv.Atoi(strings.TrimPrefix(link, "details:"))
				if err != nil {
					return
				}
				details := append([]int{}, v.document.Details...)
				found := false
				for i, value := range details {
					if value == index {
						details = append(details[:i], details[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					details = append(details, index)
				}
				v.renderMarkdown(details)
				return
			}
			if link != "" {
				v.openLink(link)
			}
		}),
		widget.WidgetOpts.OnUpdate(func(widget.HasWidget) {
			x, y := ebiten.CursorPosition()
			if cursorOver(v.GetWidget(), x, y) && v.linkAt(image.Pt(x, y)) != "" {
				w.cursorShape = ebiten.CursorShapePointer
			}
		}),
	))
	return v
}

func (v *markdownView) clearTextures() {
	for _, texture := range v.textures {
		texture.Deallocate()
	}
	v.textures = make(map[int]*ebiten.Image)
	v.loading = make(map[int]bool)
}

func (v *markdownView) setPlain(value string) {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	v.generation++
	v.document = nil
	v.native = nil
	v.clearTextures()
	v.RemoveChildren()
	v.plain = v.app.label(value)
	v.plain.MaxWidth = float64(v.preferredWidth())
	v.plain.SetPosition(&widget.TextPositioning{HTextPosition: widget.TextPositionStart, VTextPosition: widget.TextPositionStart})
	v.AddChild(v.plain)
	v.RequestRelayout()
}

func (v *markdownView) preferredWidth() int {
	return max(v.app.px(100), min(v.app.px(560), v.app.width-v.app.px(128)))
}

func (v *markdownView) PreferredSize() (int, int) {
	if v.native != nil {
		return v.native.PreferredSize()
	}
	if v.document != nil {
		return v.document.Width, v.document.Height
	}
	if v.plain == nil {
		return v.preferredWidth(), v.app.px(24)
	}
	v.plain.MaxWidth = float64(v.preferredWidth())
	_, height := v.plain.PreferredSize()
	return v.preferredWidth(), height
}

func (v *markdownView) useNative(blocks []nativeMarkdownBlock, reason string) {
	v.generation++
	v.document = nil
	v.plain = nil
	v.clearTextures()
	v.RemoveChildren()
	reasonRunes := []rune(reason)
	if len(reasonRunes) > 160 {
		reason = string(reasonRunes[:160]) + "…"
	}
	notice := nativeMarkdownBlock{notice: true, spans: []nativeMarkdownSpan{{value: "已切换原生渲染。" + reason + "\n公式、Mermaid 和 HTML 显示原文，图片显示替代文字。"}}}
	v.native = &nativeMarkdown{Container: widget.NewContainer(), app: v.app, blocks: append([]nativeMarkdownBlock{notice}, blocks...), viewport: v.viewport}
	v.AddChild(v.native)
	v.RequestRelayout()
}

func (v *markdownView) setMarkdown(source, baseURL string) {
	if strings.TrimSpace(source) == "" {
		v.setPlain("暂无发布说明。")
		return
	}
	v.source, v.baseURL = source, baseURL
	v.setPlain("正在排版发布说明（含表格、公式和图表）…")
	v.renderMarkdown(nil)
}

func (v *markdownView) renderMarkdown(details []int) {
	w := v.app
	w.cancelUpdateCheck()
	if v.cancel != nil {
		v.cancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	v.cancel = cancel
	w.updateCheckCancel = cancel
	v.generation++
	generation := v.generation
	owner := w.modalWindow
	position := 0
	if v.document != nil && v.viewport != nil {
		position = max(0, v.viewport().Min.Y-v.GetWidget().Rect.Min.Y)
	}
	c := w.colors()
	color := func(value uint32) string { return fmt.Sprintf("#%06x", value) }
	pack := func(r, g, b uint8) string { return color(uint32(r)<<16 | uint32(g)<<8 | uint32(b)) }
	options := core.MarkdownOptions{Source: v.source, BaseURL: v.baseURL, Width: v.preferredWidth(), Scale: w.displayScale, Dark: !w.lightTheme(), Details: details,
		Colors: map[string]string{"bg": pack(c.bg.R, c.bg.G, c.bg.B), "text": pack(c.text.R, c.text.G, c.text.B), "muted": pack(c.muted.R, c.muted.G, c.muted.B), "line": pack(c.line.R, c.line.G, c.line.B), "accent": pack(c.accent.R, c.accent.G, c.accent.B), "elevated": pack(c.elevated.R, c.elevated.G, c.elevated.B), "surface": pack(c.surface.R, c.surface.G, c.surface.B)}}
	w.updateRequests++
	go func() {
		defer cancel()
		document, err := core.RenderMarkdown(ctx, options)
		var first image.Image
		if err == nil && len(document.Tiles) > 0 {
			first, err = png.Decode(bytes.NewReader(document.Tiles[0].PNG))
		}
		var fallback []nativeMarkdownBlock
		if err != nil && ctx.Err() == nil {
			fallback = parseNativeMarkdown(options.Source, options.BaseURL)
		}
		cancelled := ctx.Err() != nil
		w.post(func() {
			w.updateRequests--
			if v.generation != generation || w.modalWindow != owner || cancelled {
				return
			}
			w.updateCheckCancel = nil
			v.cancel = nil
			if err != nil {
				v.useNative(fallback, err.Error())
			} else {
				v.clearTextures()
				v.RemoveChildren()
				v.plain = nil
				v.native = nil
				v.document = document
				if first != nil {
					v.textures[0] = ebiten.NewImageFromImage(first)
				}
			}
			if v.scrollTo != nil {
				v.scrollTo(float64(position))
			}
			v.RequestRelayout()
			owner.RequestRelayout()
			w.renderDirty = true
		})
	}()
}

func (v *markdownView) linkAt(position image.Point) string {
	if v.native != nil {
		return v.native.linkAt(position)
	}
	if v.document == nil || v.viewport != nil && !position.In(v.viewport()) {
		return ""
	}
	point := position.Sub(v.GetWidget().Rect.Min)
	for _, link := range v.document.Links {
		if float64(point.X) >= link.X && float64(point.X) < link.X+link.Width && float64(point.Y) >= link.Y && float64(point.Y) < link.Y+link.Height {
			return link.URL
		}
	}
	return ""
}

func (v *markdownView) Render(screen *ebiten.Image) {
	v.Container.Render(screen)
	if v.document == nil || !v.GetWidget().IsVisible() {
		return
	}
	origin := v.GetWidget().Rect.Min
	clip := screen.Bounds().Intersect(v.GetWidget().Rect)
	if v.viewport != nil {
		clip = clip.Intersect(v.viewport())
	}
	if clip.Empty() {
		return
	}
	screen = screen.SubImage(clip).(*ebiten.Image)
	firstVisible := max(0, (clip.Min.Y-origin.Y)/1024)
	for index, texture := range v.textures {
		if index < firstVisible-1 || index > firstVisible+1 {
			texture.Deallocate()
			delete(v.textures, index)
		}
	}
	for index, tile := range v.document.Tiles {
		if origin.Y+tile.Y+tile.Height <= clip.Min.Y || origin.Y+tile.Y >= clip.Max.Y {
			continue
		}
		if texture := v.textures[index]; texture != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(origin.X), float64(origin.Y+tile.Y))
			screen.DrawImage(texture, op)
		} else if !v.loading[index] {
			v.loading[index] = true
			document := v.document
			generation, owner := v.generation, v.app.modalWindow
			source, baseURL := v.source, v.baseURL
			go func(index int) {
				pixels, err := png.Decode(bytes.NewReader(document.Tiles[index].PNG))
				var fallback []nativeMarkdownBlock
				if err != nil {
					fallback = parseNativeMarkdown(source, baseURL)
				}
				v.app.post(func() {
					if v.document != document || v.generation != generation {
						return
					}
					delete(v.loading, index)
					if v.app.modalWindow != owner || v.app.ctx.Err() != nil {
						return
					}
					if err == nil {
						v.textures[index] = ebiten.NewImageFromImage(pixels)
					} else {
						position := 0
						if v.viewport != nil {
							position = max(0, v.viewport().Min.Y-v.GetWidget().Rect.Min.Y)
						}
						v.useNative(fallback, "读取说明图像失败："+err.Error())
						if v.scrollTo != nil {
							v.scrollTo(float64(position))
						}
						if owner != nil {
							owner.RequestRelayout()
						}
					}
					v.app.renderDirty = true
				})
			}(index)
		}
	}
}

func markdownScrollPosition(y float64, height, viewport int) float64 {
	if height <= viewport {
		return 0
	}
	return math.Max(0, math.Min(1, y/float64(height-viewport)))
}
