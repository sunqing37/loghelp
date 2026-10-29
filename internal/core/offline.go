package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Offline struct {
	Path       string
	Files      []string
	Anchor     time.Time
	Warnings   []string
	Bytes      int64
	warningMu  sync.Mutex
	files      []os.FileInfo
	points     []offlinePoint
	stride     uint64
	count      int
	reference  time.Time
	cacheMu    sync.Mutex
	indexes    []*offlineQueryIndex
	closeMu    sync.RWMutex
	closed     bool
	tempDir    string
	origins    map[string]logOrigin
	jsonFields JSONLFields
}

func OpenOffline(ctx context.Context, path string) (*Offline, error) {
	return OpenOfflineProgress(ctx, path, nil)
}

func OpenOfflineProgress(ctx context.Context, path string, progress func(int64)) (*Offline, error) {
	return openOfflineProgress(ctx, path, progress, JSONLFields{})
}

func openOfflineProgress(ctx context.Context, path string, progress func(int64), fields JSONLFields) (*Offline, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	o := &Offline{Path: abs, stride: 512, reference: time.Now(), jsonFields: fields}
	manifest := abs
	if info.IsDir() {
		manifest = filepath.Join(abs, "manifest.json")
	}
	if info.IsDir() || filepath.Base(abs) == "manifest.json" {
		data, err := os.ReadFile(manifest)
		if err != nil {
			return nil, err
		}
		var m Manifest
		if err = json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if m.Format != "loghelp" || m.Version != 1 {
			return nil, fmt.Errorf("不支持的会话文件格式")
		}
		root := filepath.Dir(manifest)
		for _, name := range m.Segments {
			if filepath.Base(name) != name || !strings.HasSuffix(name, ".jsonl") {
				return nil, fmt.Errorf("无效片段路径 %s", name)
			}
			o.Files = append(o.Files, filepath.Join(root, name))
		}
	} else {
		o.Files = []string{abs}
	}
	return indexOffline(ctx, o, progress)
}

func indexOffline(ctx context.Context, o *Offline, progress func(int64)) (*Offline, error) {
	if len(o.Files) == 0 {
		return nil, fmt.Errorf("会话中没有日志片段")
	}
	for _, file := range o.Files {
		st, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		o.Bytes += st.Size()
		o.files = append(o.files, st)
	}
	// Build sparse byte offsets while obtaining the fixed age anchor. No log
	// text is retained; checkpoints compact as the source grows.
	err := o.scan(ctx, 1, true, func(e *Entry) bool {
		o.count++
		if !e.Time.IsZero() && !e.Event {
			o.Anchor = e.Time
		}
		return true
	}, progress)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// Close waits for current readers before removing only this import's temporary
// files. Plain files and recording sessions are never removed.
func (o *Offline) Close() error {
	if o == nil {
		return nil
	}
	o.closeMu.Lock()
	defer o.closeMu.Unlock()
	o.closed = true
	if o.tempDir != "" {
		if err := os.RemoveAll(o.tempDir); err != nil {
			return fmt.Errorf("清理日志临时目录：%w", err)
		}
		o.tempDir = ""
	}
	return nil
}
func (o *Offline) Scan(ctx context.Context, visit func(*Entry) bool, progress func(int64)) error {
	return o.scan(ctx, 1, false, visit, progress)
}

type offlinePoint struct {
	Seq                      uint64
	File, Line               int
	Offset, Consumed         int64
	MaxTime                  time.Time
	RecordedMin, RecordedMax uint64
}

const maxOfflinePoints = 8192

type countedReader struct {
	io.Reader
	count int64
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (o *Offline) scan(ctx context.Context, first uint64, build bool, visit func(*Entry) bool, progress func(int64)) error {
	o.closeMu.RLock()
	defer o.closeMu.RUnlock()
	if o.closed {
		return fmt.Errorf("日志已关闭，请重新打开")
	}
	point := offlinePoint{Seq: 1}
	if !build && len(o.points) > 0 {
		i := sort.Search(len(o.points), func(i int) bool { return o.points[i].Seq > first }) - 1
		point = o.points[max(i, 0)]
	}
	seq, consumed := point.Seq-1, point.Consumed
	for fileIndex := point.File; fileIndex < len(o.Files); fileIndex++ {
		path := o.Files[fileIndex]
		origin := o.origins[path]
		if origin.name == "" {
			origin.name = path
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		info, statErr := f.Stat()
		original := o.files[fileIndex]
		if statErr != nil || !os.SameFile(original, info) || info.Size() < original.Size() || info.Size() == original.Size() && !info.ModTime().Equal(original.ModTime()) {
			f.Close()
			return fmt.Errorf("日志文件已改变或无法读取，请重新打开：%s", path)
		}
		var offset int64
		lineNumber := 0
		if fileIndex == point.File {
			offset, lineNumber = point.Offset, point.Line
		}
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return err
		}
		counter := &countedReader{Reader: io.LimitReader(f, original.Size()-offset)}
		reader := bufio.NewReaderSize(counter, 128<<10)
		fileStart := consumed - offset
		for {
			if err = ctx.Err(); err != nil {
				f.Close()
				return err
			}
			lineOffset, lineConsumed := offset, consumed
			line, readErr, truncated := boundedLineContext(ctx, reader)
			if err := ctx.Err(); err != nil {
				f.Close()
				return err
			}
			offset = pointOffset(fileIndex, point) + counter.count - int64(reader.Buffered())
			consumed = fileStart + offset
			if truncated {
				o.warn(fmt.Sprintf("%s 第 %d 行超过 16 MiB，显示截断预览；原始文件未修改", origin.name, lineNumber+1))
			}
			lineNumber++
			if len(line) > 0 {
				var e *Entry
				raw := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
				if strings.EqualFold(filepath.Ext(path), ".jsonl") {
					var decodeErr error
					e, decodeErr = parseJSONLine(raw, o.reference, o.jsonFields)
					if decodeErr != nil {
						o.warn(fmt.Sprintf("%s 第 %d 行：%s", origin.name, lineNumber, decodeErr))
					}
				} else {
					e = ParseLine(raw, o.reference)
				}
				e.SourceFile, e.SourceLine, e.SourceDecoded = origin.name, lineNumber, origin.decoded
				seq++
				e.Seq = seq
				if build {
					o.addPoint(e, fileIndex, lineNumber-1, lineOffset, lineConsumed)
				}
				if e.Seq >= first && !visit(e) {
					f.Close()
					if readErr != nil && readErr != io.EOF {
						return readErr
					}
					return ctx.Err()
				}
			}
			if progress != nil && lineNumber%4096 == 0 {
				progress(consumed)
			}
			if readErr != nil {
				if readErr != io.EOF {
					f.Close()
					return readErr
				}
				break
			}
		}
		f.Close()
	}
	if progress != nil {
		progress(consumed)
	}
	return ctx.Err()
}

func pointOffset(file int, p offlinePoint) int64 {
	if file == p.File {
		return p.Offset
	}
	return 0
}

func (o *Offline) addPoint(e *Entry, file, line int, offset, consumed int64) {
	if (e.Seq-1)%o.stride == 0 {
		o.points = append(o.points, offlinePoint{Seq: e.Seq, File: file, Line: line, Offset: offset, Consumed: consumed})
		if len(o.points) > maxOfflinePoints {
			for i := 0; i < len(o.points); i += 2 {
				p := o.points[i]
				if i+1 < len(o.points) {
					mergeOfflinePoint(&p, o.points[i+1])
				}
				o.points[i/2] = p
			}
			o.points = o.points[:(len(o.points)+1)/2]
			o.stride *= 2
		}
	}
	p := &o.points[len(o.points)-1]
	if e.Time.After(p.MaxTime) {
		p.MaxTime = e.Time
	}
	if e.RecordedSeq != 0 {
		if p.RecordedMin == 0 || e.RecordedSeq < p.RecordedMin {
			p.RecordedMin = e.RecordedSeq
		}
		p.RecordedMax = max(p.RecordedMax, e.RecordedSeq)
	}
}

func mergeOfflinePoint(a *offlinePoint, b offlinePoint) {
	if b.MaxTime.After(a.MaxTime) {
		a.MaxTime = b.MaxTime
	}
	if b.RecordedMin != 0 && (a.RecordedMin == 0 || b.RecordedMin < a.RecordedMin) {
		a.RecordedMin = b.RecordedMin
	}
	a.RecordedMax = max(a.RecordedMax, b.RecordedMax)
}
func (o *Offline) warn(message string) {
	o.warningMu.Lock()
	defer o.warningMu.Unlock()
	for _, w := range o.Warnings {
		if w == message {
			return
		}
	}
	if len(o.Warnings) < 20 {
		o.Warnings = append(o.Warnings, message)
	}
}
func (o *Offline) WarningList() []string {
	o.warningMu.Lock()
	defer o.warningMu.Unlock()
	return append([]string(nil), o.Warnings...)
}

type Page struct {
	Rows               []*Entry
	Total, SearchCount int
	Hits               []int
	Offset             int
	Dropped            uint64
	Version            uint64
	Anchor             time.Time
}

func (o *Offline) Query(ctx context.Context, q Query, search Search, pkg string, offset, limit int, progress func(int64)) (Page, error) {
	index, err := o.queryIndex(ctx, q, search, pkg, progress)
	if err != nil {
		return Page{}, err
	}
	offset, limit = max(offset, 0), max(0, min(limit, 2000))
	result := Page{Offset: offset, Anchor: o.Anchor, Total: index.Total, SearchCount: index.Hits}
	if offset >= index.Total || limit == 0 {
		return result, ctx.Err()
	}
	block := index.points[max(0, sort.Search(len(index.points), func(i int) bool { return index.points[i].Offset > offset })-1)]
	current := block.Offset
	err = o.scan(ctx, block.Seq, false, func(e *Entry) bool {
		if !q.Match(e, o.Anchor) || pkg != "" && e.Package != pkg {
			return true
		}
		if current >= offset {
			if len(search.Matches(e.Raw)) > 0 {
				result.Hits = append(result.Hits, len(result.Rows))
			}
			result.Rows = append(result.Rows, e)
		}
		current++
		return current < index.Total && len(result.Rows) < limit
	}, progress)
	return result, err
}
func (o *Offline) Find(ctx context.Context, q Query, search Search, pkg string, after int, reverse bool) (int, error) {
	return o.FindProgress(ctx, q, search, pkg, after, reverse, nil)
}
func (o *Offline) Context(ctx context.Context, seq uint64, before, after int) ([]*Entry, error) {
	return o.ContextProgress(ctx, seq, before, after, nil)
}

func (o *Offline) ContextProgress(ctx context.Context, seq uint64, before, after int, progress func(int64)) ([]*Entry, error) {
	before, after = max(0, min(before, 1000)), max(0, min(after, 2000))
	var rows []*Entry
	first := uint64(1)
	if seq > uint64(before) {
		first = seq - uint64(before)
	}
	err := o.scan(ctx, first, false, func(e *Entry) bool {
		if e.Seq >= first && e.Seq <= seq+uint64(after) {
			rows = append(rows, ContextPreview(e))
		}
		return e.Seq < seq+uint64(after)
	}, progress)
	return rows, err
}

// Context panels show bounded text previews. Full originals remain available
// in the log detail/export paths, even for unusually large individual records.
func ContextPreview(e *Entry) *Entry {
	preview := &Entry{Seq: e.Seq, RecordedSeq: e.RecordedSeq, Time: e.Time, Received: e.Received, Generation: e.Generation, Event: e.Event}
	preview.Raw = strings.Clone(truncateUTF8(e.Raw, 4096))
	if len(preview.Raw) < len(e.Raw) {
		preview.Raw += " … [上下文预览截断，请在日志详情查看原文]"
	}
	return preview
}
func QueryLive(entries []*Entry, q Query, search Search, pkg string, after uint64) Page {
	result := Page{Anchor: time.Now()}
	// Ring snapshots and paused/stopped views are immutable. The unfiltered
	// result can share their pointer array instead of allocating a second one.
	if strings.TrimSpace(q.Text) == "" && pkg == "" && search.Text == "" {
		first := 0
		for first < len(entries) && entries[first].Seq <= after {
			first++
		}
		result.Rows = entries[first:]
		result.Total = len(result.Rows)
		return result
	}
	for _, e := range entries {
		if e.Seq <= after || !q.Match(e, result.Anchor) || pkg != "" && e.Package != pkg {
			continue
		}
		if hits := search.Matches(e.Raw); len(hits) > 0 {
			result.Hits = append(result.Hits, len(result.Rows))
			result.SearchCount += len(hits)
		}
		result.Rows = append(result.Rows, e)
	}
	result.Total = len(result.Rows)
	return result
}
func Export(ctx context.Context, path string, jsonLines bool, scan func(func(*Entry) bool) error) (int, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	writer := bufio.NewWriterSize(f, 128<<10)
	count := 0
	var writeErr error
	err = scan(func(e *Entry) bool {
		if ctx.Err() != nil {
			writeErr = ctx.Err()
			return false
		}
		var data []byte
		if jsonLines {
			data, writeErr = json.Marshal(e)
		} else {
			data = []byte(e.Raw)
		}
		if writeErr == nil {
			_, writeErr = writer.Write(append(data, '\n'))
		}
		if writeErr == nil {
			count++
		}
		return writeErr == nil
	})
	if writeErr != nil {
		err = writeErr
	}
	if flushErr := writer.Flush(); err == nil {
		err = flushErr
	}
	if syncErr := f.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return count, err
}
