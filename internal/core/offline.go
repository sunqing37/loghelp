package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Offline struct {
	Path      string
	Files     []string
	Anchor    time.Time
	Warnings  []string
	Bytes     int64
	warningMu sync.Mutex
}

func OpenOffline(ctx context.Context, path string) (*Offline, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	o := &Offline{Path: abs}
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
	if len(o.Files) == 0 {
		return nil, fmt.Errorf("会话中没有日志片段")
	}
	for _, file := range o.Files {
		st, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		o.Bytes += st.Size()
	}
	// Streaming once obtains the fixed age anchor; memory is independent of file size.
	err = o.Scan(ctx, func(e *Entry) bool {
		if !e.Time.IsZero() && !e.Event {
			o.Anchor = e.Time
		}
		return true
	}, nil)
	if err != nil {
		return nil, err
	}
	return o, nil
}
func (o *Offline) Scan(ctx context.Context, visit func(*Entry) bool, progress func(int64)) error {
	var seq uint64
	var consumed int64
	for _, path := range o.Files {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		reader := bufio.NewReaderSize(f, 128<<10)
		lineNumber := 0
		for {
			if err = ctx.Err(); err != nil {
				f.Close()
				return err
			}
			line, readErr, truncated := boundedLine(reader)
			if truncated {
				o.warn(fmt.Sprintf("%s 第 %d 行超过 16 MiB，显示截断预览；原始文件未修改", filepath.Base(path), lineNumber+1))
			}
			lineNumber++
			consumed += int64(len(line))
			if len(line) > 0 {
				var e *Entry
				raw := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
				if strings.HasSuffix(path, ".jsonl") {
					var parsed Entry
					if decodeErr := json.Unmarshal([]byte(raw), &parsed); decodeErr != nil {
						if readErr == io.EOF {
							o.warn(fmt.Sprintf("%s 第 %d 行尾部损坏：保留此前有效条目", filepath.Base(path), lineNumber))
							break
						}
						parsed = *ParseLine(raw, time.Now())
						o.warn(fmt.Sprintf("%s 第 %d 行无法解析，按原文展示", filepath.Base(path), lineNumber))
					}
					e = &parsed
				} else {
					e = ParseLine(raw, time.Now())
				}
				seq++
				e.Seq = seq
				if !visit(e) {
					f.Close()
					return nil
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
	return nil
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
	result := Page{Offset: offset, Anchor: o.Anchor}
	err := o.Scan(ctx, func(e *Entry) bool {
		if q.Match(e, o.Anchor) && (pkg == "" || e.Package == pkg) {
			index := result.Total
			result.Total++
			hits := search.Matches(e.Raw)
			result.SearchCount += len(hits)
			if index >= offset && len(result.Rows) < limit {
				if len(hits) > 0 {
					result.Hits = append(result.Hits, len(result.Rows))
				}
				result.Rows = append(result.Rows, e)
			}
		}
		return true
	}, progress)
	return result, err
}
func (o *Offline) Find(ctx context.Context, q Query, search Search, pkg string, after int, reverse bool) (int, error) {
	index := 0
	found := -1
	err := o.Scan(ctx, func(e *Entry) bool {
		if !q.Match(e, o.Anchor) || pkg != "" && e.Package != pkg {
			return true
		}
		current := index
		index++
		if len(search.Matches(e.Raw)) == 0 {
			return true
		}
		if reverse {
			if current >= after {
				return false
			}
			found = current
			return true
		}
		if current > after {
			found = current
			return false
		}
		return true
	}, nil)
	return found, err
}
func (o *Offline) Context(ctx context.Context, seq uint64, before, after int) ([]*Entry, error) {
	var rows []*Entry
	first := uint64(1)
	if seq > uint64(before) {
		first = seq - uint64(before)
	}
	err := o.Scan(ctx, func(e *Entry) bool {
		if e.Seq >= first && e.Seq <= seq+uint64(after) {
			rows = append(rows, e)
		}
		return e.Seq < seq+uint64(after)
	}, nil)
	return rows, err
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
