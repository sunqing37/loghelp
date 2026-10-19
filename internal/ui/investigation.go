package ui

import (
	"fmt"
	"github.com/atotto/clipboard"
	"github.com/ebitenui/ebitenui/widget"
	"loghelp/internal/core"
	"loghelp/internal/support"
	"strings"
)

func (w *app) viewJSON() {
	p := w.active()
	if selectedEntry(p) == nil {
		w.message("JSON 查看", "请先选择包含 JSON 的日志。")
		return
	}
	raw := p.detailEntry(p.selected).Raw
	n, err := core.ParseLogJSON(raw)
	if err != nil {
		w.message("JSON 查看", err.Error())
		return
	}
	expanded := map[string]bool{"": true}
	var show func(int)
	show = func(page int) {
		box := w.column(w.row(w.button("原文", func() { w.previewReportFile(support.ReportFile{Name: "原始日志", Data: []byte(raw)}) }), w.button("格式化全文", func() { w.previewReportFile(support.ReportFile{Name: "JSON", Data: []byte(n.Pretty())}) }), w.button("复制 JSON", func() {
			if err := clipboard.WriteAll(n.Pretty()); err != nil {
				w.message("复制失败", err.Error())
			}
		})))
		type row struct {
			n     *core.JSONNode
			depth int
		}
		var visible []row
		var walk func(*core.JSONNode, int)
		walk = func(n *core.JSONNode, d int) {
			visible = append(visible, row{n, d})
			if expanded[n.Path] {
				for _, child := range n.Children {
					walk(child, d+1)
				}
			}
		}
		walk(n, 0)
		page = min(page, max(0, (len(visible)-1)/40))
		for _, r := range visible[page*40 : min((page+1)*40, len(visible))] {
			r := r
			prefix := ""
			if len(r.n.Children) > 0 {
				prefix = "+ "
				if expanded[r.n.Path] {
					prefix = "− "
				}
			}
			value := shortText(r.n.CopyValue(), 80)
			if len(r.n.Children) > 0 {
				value = fmt.Sprintf("%d 项", len(r.n.Children))
			}
			label := strings.Repeat("  ", min(r.depth, 8)) + prefix + shortText(r.n.Name, 50) + ": " + value
			var expand *widget.Button
			expand = w.button(w.wirelessText(label, w.wirelessContentWidth()-w.px(130)), func() {
				if len(r.n.Children) > 0 {
					expanded[r.n.Path] = !expanded[r.n.Path]
					show(page)
				} else {
					w.previewReportFile(support.ReportFile{Name: r.n.Path, Data: []byte(r.n.CopyValue())})
				}
			})
			box.AddChild(w.row(expand, w.button("复制值", func() {
				if err := clipboard.WriteAll(r.n.CopyValue()); err != nil {
					w.message("复制失败", err.Error())
				}
			})))
		}
		box.AddChild(w.label(fmt.Sprintf("第 %d / %d 页 · 点击层级折叠/展开；叶节点可查看完整值", page+1, max(1, (len(visible)+39)/40))))
		if page > 0 {
			box.AddChild(w.button("上一页", func() { show(page - 1) }))
		}
		if (page+1)*40 < len(visible) {
			box.AddChild(w.button("下一页", func() { show(page + 1) }))
		}
		w.show("JSON 格式化查看", box)
	}
	show(0)
}
