package ui

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"loghelp/internal/core"
)

type logGroup struct{ start, end int }

const (
	maxGroupLines     = 512
	maxGroupBytes     = 256 << 10
	minJSONChunkBytes = 3 << 10
	maxJSONGapMillis  = 10
)

func groupEntryBytes(entry *core.Entry) int {
	return max(len(entry.Raw), len(entry.Message))
}

func sameLogBlock(a, b *core.Entry) bool {
	return a.Time.UnixMilli() == b.Time.UnixMilli() && sameLogSource(a, b)
}

func sameLogSource(a, b *core.Entry) bool {
	return !a.Event && !b.Event && !a.Time.IsZero() && !b.Time.IsZero() &&
		a.PID > 0 && a.TID > 0 && a.Tag != "" &&
		b.Seq == a.Seq+1 &&
		a.SourceFile == b.SourceFile && a.SourceDecoded == b.SourceDecoded &&
		(a.SourceFile == "" || b.SourceLine == a.SourceLine+1) &&
		a.Serial == b.Serial && a.Generation == b.Generation && a.Buffer == b.Buffer &&
		a.PID == b.PID && a.TID == b.TID && a.Tag == b.Tag && a.Level == b.Level &&
		a.Package == b.Package && a.Process == b.Process
}

func jsonLogHeader(message string) (header, prefix string) {
	header = message
	if strings.HasPrefix(header, "[") {
		if at := strings.Index(header, "]:"); at >= 0 && strings.Count(header[:at], ",") == 2 {
			prefix, header = header[:at+2], header[at+2:]
		}
	}
	return header, prefix
}

func jsonQuoteState(text string, quoted, escaped bool) (bool, bool) {
	for _, c := range text {
		if escaped {
			escaped = false
		} else if quoted && c == '\\' {
			escaped = true
		} else if c == '"' {
			quoted = !quoted
		}
	}
	return quoted, escaped
}

// Only bridge long, incomplete JSON bodies. A short time gap alone cannot
// distinguish separate log calls on the same thread. Look ahead to a complete
// JSON value before accepting any timestamp boundary; incomplete captures stay
// split. Raw entries are never rewritten.
func extendJSONLogGroup(rows []*core.Entry, start, end, bytes int) (int, int) {
	originalEnd, originalBytes := end, bytes
	if end == len(rows) || bytes < minJSONChunkBytes || !sameLogSource(rows[end-1], rows[end]) {
		return end, bytes
	}
	// Preserve and match the logger's source-location prefix on each chunk.
	header, prefix := jsonLogHeader(rows[start].Message)
	bodyStart := start
	if label := strings.TrimSpace(header); label == "response body:" || label == "request body:" {
		bodyStart++
	} else if prefix != "" {
		return end, bytes
	}
	if bodyStart == end {
		return end, bytes
	}
	first := strings.TrimSpace(rows[bodyStart].Message)
	if !strings.HasPrefix(first, "{") && !strings.HasPrefix(first, "[") {
		return end, bytes
	}
	var body strings.Builder
	for i := bodyStart; i < end; i++ {
		if i > bodyStart {
			body.WriteByte('\n')
		}
		body.WriteString(rows[i].Message)
	}
	if body.Len() < minJSONChunkBytes {
		return end, bytes
	}
	for {
		var value json.RawMessage
		if err := json.NewDecoder(strings.NewReader(body.String())).Decode(&value); err != io.ErrUnexpectedEOF {
			if err == nil && end > originalEnd {
				return end, bytes
			}
			break
		}
		if end == len(rows) || end-start == maxGroupLines {
			break
		}
		a, b := rows[end-1], rows[end]
		gap := b.Time.UnixMilli() - a.Time.UnixMilli()
		if !sameLogSource(a, b) || gap <= 0 || b.Time.UnixMilli()-rows[start].Time.UnixMilli() > maxJSONGapMillis {
			break
		}
		message := b.Message
		if prefix != "" {
			if !strings.HasPrefix(message, prefix) {
				break
			}
			message = strings.TrimPrefix(message, prefix)
		}
		if label := strings.TrimSpace(message); strings.HasPrefix(label, "response body:") || strings.HasPrefix(label, "request body:") {
			break
		}
		// A chunk split inside a JSON string must be rejoined without a
		// newline. Outside a string, retain whitespace between JSON tokens.
		quoted, _ := jsonQuoteState(body.String(), false, false)
		if !quoted {
			body.WriteByte('\n')
		}
		next := end
		for next < len(rows) && (next == end || sameLogBlock(rows[next-1], rows[next])) {
			nextBytes := groupEntryBytes(rows[next]) + 1
			if next-start >= maxGroupLines || bytes+nextBytes > maxGroupBytes {
				return originalEnd, originalBytes
			}
			bytes += nextBytes
			if next == end {
				body.WriteString(message)
			} else {
				body.WriteByte('\n')
				body.WriteString(rows[next].Message)
			}
			next++
		}
		end = next
	}
	return originalEnd, originalBytes
}

func mergeLogRows(ctx context.Context, source core.Page, scratch []logGroup, rowScratch []*core.Entry) (core.Page, []logGroup, error) {
	// Keep the common no-merge case allocation-free. Groups contain only
	// indices; full multiline text is built lazily for the selected group.
	canMerge := false
	for i := 1; i < len(source.Rows); i++ {
		if i%1024 == 0 && ctx.Err() != nil {
			return source, nil, ctx.Err()
		}
		a, b := source.Rows[i-1], source.Rows[i]
		gap := b.Time.UnixMilli() - a.Time.UnixMilli()
		longChunk := groupEntryBytes(a) >= minJSONChunkBytes && gap > 0 && gap <= maxJSONGapMillis && sameLogSource(a, b)
		if (sameLogBlock(a, b) || longChunk) && groupEntryBytes(a)+groupEntryBytes(b)+1 <= maxGroupBytes {
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
		end, bytes = extendJSONLogGroup(source.Rows, start, end, bytes)
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

// Query results and merged groups preserve the source's sequence order.
func (p *page) rowAtOrAfterSeq(seq uint64) int {
	return sort.Search(len(p.result.Rows), func(i int) bool {
		return p.rowLastSeq(i) >= seq
	})
}

func (p *page) rowContainingSeq(seq uint64) int {
	if seq == 0 {
		return -1
	}
	index := p.rowAtOrAfterSeq(seq)
	if index == len(p.result.Rows) || p.result.Rows[index].Seq > seq {
		return -1
	}
	return index
}

// Explicit selection starts a new anchor. Automatic regrouping keeps this
// original sequence even when it is no longer the displayed group's first row.
func (p *page) selectRow(index int) {
	p.selected = index
	p.selectionSeq = 0
	p.keepSelectionVisible = false
	if index >= 0 && index < len(p.result.Rows) {
		p.selectionSeq = p.result.Rows[index].Seq
	}
}

func (p *page) scrollAnchorSeq() uint64 {
	if p.scroll < 0 || p.scroll >= len(p.result.Rows) {
		return 0
	}
	if p.rowContainingSeq(p.viewportSeq) == p.scroll {
		return p.viewportSeq
	}
	return p.result.Rows[p.scroll].Seq
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
		header, prefix := jsonLogHeader(rows[0].Message)
		quoted, escaped := jsonQuoteState(header, false, false)
		for i, row := range rows[1:] {
			message := row.Message
			continuation := !sameLogBlock(rows[i], row)
			if continuation {
				message = strings.TrimPrefix(message, prefix)
			}
			if !continuation || !quoted {
				text.WriteByte('\n')
			}
			text.WriteString(message)
			quoted, escaped = jsonQuoteState(message, quoted, escaped)
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
