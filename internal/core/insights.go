package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EntryScan visits a fixed source, without retaining all offline records.
type EntryScan func(context.Context, func(*Entry) bool) error

type LogEvent struct {
	Seq                uint64
	Time               time.Time
	Kind, App, Summary string
}
type LogBucket struct {
	Second        int64
	Count, Errors int
	FirstSeq      uint64
}
type LogOverview struct {
	Total, Errors, Untimed, OtherTags, ExtraEvents int
	First, Last                                    time.Time
	Tags                                           map[string]int
	Events                                         []LogEvent
	Buckets                                        []LogBucket
	BucketSeconds                                  int64
}

func eventKind(e *Entry) string {
	s := strings.ToLower(e.Message)
	switch {
	case strings.Contains(s, "fatal exception"), strings.Contains(s, "fatal signal"), strings.Contains(s, "am_crash"):
		return "崩溃线索"
	case strings.Contains(s, "anr in "), strings.Contains(s, "am_anr"), strings.Contains(s, "input dispatching timed out"):
		return "ANR 线索"
	case e.Event && strings.Contains(s, "进程结束"), strings.Contains(s, "am_proc_died"), strings.Contains(s, "has died") && e.Tag == "ActivityManager":
		return "进程退出线索"
	}
	return ""
}

func AnalyzeLogs(ctx context.Context, scan EntryScan) (LogOverview, error) {
	r := LogOverview{Tags: map[string]int{}, BucketSeconds: 1}
	buckets := map[int64]LogBucket{}
	err := scan(ctx, func(e *Entry) bool {
		if ctx.Err() != nil {
			return false
		}
		r.Total++
		bad := LevelValue(e.Level) >= 5
		if bad {
			r.Errors++
		}
		tag := strings.Clone(truncateUTF8(e.Tag, 512))
		if tag == "" {
			tag = "（无 TAG）"
		}
		if _, ok := r.Tags[tag]; ok || len(r.Tags) < 2048 {
			r.Tags[tag]++
		} else {
			r.OtherTags++
		}
		if kind := eventKind(e); kind != "" {
			if len(r.Events) < 500 {
				r.Events = append(r.Events, LogEvent{e.Seq, e.Time, kind, strings.Clone(truncateUTF8(e.Package, 256)), strings.Clone(truncateUTF8(e.Message, 512))})
			} else {
				r.ExtraEvents++
			}
		}
		if e.Time.IsZero() || e.Event {
			r.Untimed++
			return true
		}
		if r.First.IsZero() || e.Time.Before(r.First) {
			r.First = e.Time
		}
		if r.Last.IsZero() || e.Time.After(r.Last) {
			r.Last = e.Time
		}
		second := e.Time.Unix() / r.BucketSeconds * r.BucketSeconds
		b := buckets[second]
		b.Second = second
		b.Count++
		if bad {
			b.Errors++
		}
		if b.FirstSeq == 0 || e.Seq < b.FirstSeq {
			b.FirstSeq = e.Seq
		}
		buckets[second] = b
		for len(buckets) > 512 {
			r.BucketSeconds *= 2
			merged := map[int64]LogBucket{}
			for _, old := range buckets {
				key := old.Second / r.BucketSeconds * r.BucketSeconds
				b := merged[key]
				b.Second = key
				b.Count += old.Count
				b.Errors += old.Errors
				if b.FirstSeq == 0 || old.FirstSeq < b.FirstSeq {
					b.FirstSeq = old.FirstSeq
				}
				merged[key] = b
			}
			buckets = merged
		}
		return true
	})
	if err == nil {
		err = ctx.Err()
	}
	for _, b := range buckets {
		r.Buckets = append(r.Buckets, b)
	}
	sort.Slice(r.Buckets, func(i, j int) bool { return r.Buckets[i].Second < r.Buckets[j].Second })
	return r, err
}

type JSONNode struct {
	Path, Name string
	Value      json.RawMessage
	Children   []*JSONNode
}

// ParseLogJSON keeps numbers as raw JSON and bounds both decoding and UI nodes.
func ParseLogJSON(value string) (*JSONNode, error) {
	if len(value) > 1<<20 {
		return nil, fmt.Errorf("JSON 查看最多支持 1 MiB，请缩小日志范围")
	}
	var raw json.RawMessage
	for start, attempts := 0, 0; start < len(value) && attempts < 32; attempts++ {
		i := strings.IndexAny(value[start:], "{[")
		if i < 0 {
			break
		}
		start += i
		dec := json.NewDecoder(strings.NewReader(value[start:]))
		if dec.Decode(&raw) == nil {
			break
		}
		raw = nil
		start++
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("未找到有效 JSON 对象或数组；支持带日志前缀的完整 JSON")
	}
	count := 0
	var build func(string, string, json.RawMessage, int) (*JSONNode, error)
	build = func(path, name string, v json.RawMessage, depth int) (*JSONNode, error) {
		count++
		if count > 4000 || depth > 64 {
			return nil, fmt.Errorf("JSON 超过 4000 个节点或 64 层，请缩小范围")
		}
		n := &JSONNode{Path: path, Name: name, Value: v}
		v = bytes.TrimSpace(v)
		if v[0] == '{' {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(v, &obj); err != nil {
				return nil, err
			}
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				escaped := strings.NewReplacer("~", "~0", "/", "~1").Replace(k)
				child, err := build(path+"/"+escaped, k, obj[k], depth+1)
				if err != nil {
					return nil, err
				}
				n.Children = append(n.Children, child)
			}
		} else if v[0] == '[' {
			var a []json.RawMessage
			if err := json.Unmarshal(v, &a); err != nil {
				return nil, err
			}
			for i, item := range a {
				key := fmt.Sprint(i)
				child, err := build(path+"/"+key, "["+key+"]", item, depth+1)
				if err != nil {
					return nil, err
				}
				n.Children = append(n.Children, child)
			}
		}
		return n, nil
	}
	return build("", "JSON", raw, 0)
}

func (n *JSONNode) CopyValue() string {
	var s string
	if json.Unmarshal(n.Value, &s) == nil {
		return s
	}
	return string(n.Value)
}
func (n *JSONNode) Pretty() string {
	var b bytes.Buffer
	_ = json.Indent(&b, n.Value, "", "  ")
	return b.String()
}
