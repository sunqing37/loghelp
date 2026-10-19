package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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
