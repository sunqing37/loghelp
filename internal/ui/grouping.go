package ui

import (
	"context"
	"strings"

	"loghelp/internal/core"
)

type logGroup struct{ start, end int }

const (
	maxGroupLines = 512
	maxGroupBytes = 256 << 10
)

func groupEntryBytes(entry *core.Entry) int {
	return max(len(entry.Raw), len(entry.Message))
}

func sameLogBlock(a, b *core.Entry) bool {
	return !a.Event && !b.Event && !a.Time.IsZero() && !b.Time.IsZero() &&
		a.PID > 0 && a.TID > 0 && a.Tag != "" &&
		a.Time.UnixMilli() == b.Time.UnixMilli() && b.Seq == a.Seq+1 &&
		a.Serial == b.Serial && a.Generation == b.Generation && a.Buffer == b.Buffer &&
		a.PID == b.PID && a.TID == b.TID && a.Tag == b.Tag && a.Level == b.Level &&
		a.Package == b.Package && a.Process == b.Process
}

func mergeLogRows(ctx context.Context, source core.Page, scratch []logGroup, rowScratch []*core.Entry) (core.Page, []logGroup, error) {
	// Keep the common no-merge case allocation-free. Groups contain only
	// indices; full multiline text is built lazily for the selected group.
	canMerge := false
	for i := 1; i < len(source.Rows); i++ {
		if i%1024 == 0 && ctx.Err() != nil {
			return source, nil, ctx.Err()
		}
		if sameLogBlock(source.Rows[i-1], source.Rows[i]) && groupEntryBytes(source.Rows[i-1])+groupEntryBytes(source.Rows[i])+1 <= maxGroupBytes {
			canMerge = true
			break
		}
	}
	if !canMerge {
		return source, nil, ctx.Err()
	}
	result := source
	result.Rows, result.Hits = rowScratch[:0], nil
	groups := scratch[:0]
	hit := 0
	for start := 0; start < len(source.Rows); {
		if ctx.Err() != nil {
			return source, nil, ctx.Err()
		}
		end, bytes := start+1, groupEntryBytes(source.Rows[start])
		for end < len(source.Rows) && end-start < maxGroupLines && sameLogBlock(source.Rows[end-1], source.Rows[end]) {
			nextBytes := groupEntryBytes(source.Rows[end]) + 1
			if bytes+nextBytes > maxGroupBytes {
				break
			}
			bytes += nextBytes
			end++
		}
		for hit < len(source.Hits) && source.Hits[hit] < start {
			hit++
		}
		if hit < len(source.Hits) && source.Hits[hit] < end {
			result.Hits = append(result.Hits, len(groups))
		}
		groups = append(groups, logGroup{start: start, end: end})
		result.Rows = append(result.Rows, source.Rows[start])
		start = end
	}
	return result, groups, nil
}

func (p *page) rowRange(index int) (int, int) {
	if len(p.groups) == 0 {
		return index, index + 1
	}
	return p.groups[index].start, p.groups[index].end
}

func (p *page) rowMembers(index int) []*core.Entry {
	if len(p.groups) == 0 {
		return p.result.Rows[index : index+1]
	}
	start, end := p.rowRange(index)
	return p.rawResult.Rows[start:end]
}

func (p *page) rowLastSeq(index int) uint64 {
	rows := p.rowMembers(index)
	return rows[len(rows)-1].Seq
}

func (p *page) detailEntry(index int) *core.Entry {
	rows := p.rowMembers(index)
	if len(rows) == 1 {
		p.groupDetail, p.groupDetailSource = nil, nil
		return rows[0]
	}
	last := rows[len(rows)-1].Seq
	if p.groupDetail == nil || p.groupDetailSource != rows[0] || p.groupDetailLastSeq != last {
		entry := *rows[0]
		var text strings.Builder
		text.WriteString(entry.Raw)
		for _, row := range rows[1:] {
			text.WriteByte('\n')
			text.WriteString(row.Message)
		}
		entry.Raw = text.String()
		p.groupDetail, p.groupDetailLastSeq = &entry, last
		p.groupDetailSource = rows[0]
	}
	return p.groupDetail
}

func (p *page) selectedRaw() string {
	rows := p.rowMembers(p.selected)
	if len(rows) == 1 {
		return rows[0].Raw
	}
	var raw strings.Builder
	for i, row := range rows {
		if i > 0 {
			raw.WriteByte('\n')
		}
		raw.WriteString(row.Raw)
	}
	return raw.String()
}

func (w *app) toggleMergeLines() {
	p := w.active()
	if p == nil || p.shell != nil {
		return
	}
	w.syncInputs()
	p.state.MergeLines = !p.state.MergeLines
	p.keepSelectionVisible = true
	p.detailScroll, p.detailX = 0, 0
	p.groupDetail = nil
	p.groupDetailSource = nil
	p.detailCache = detailTextCache{}
	w.refresh(p, true)
	w.rebuild()
	w.save()
}
