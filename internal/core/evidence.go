package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"loghelp/internal/support"
)

type EvidenceOptions struct {
	From, To                     time.Time
	Note, Package, Serial, Scope string
	Redact                       bool
	Info                         map[string]string
	Screenshot                   *ScreenFrame
}

// Freeze every exported byte before preview. Never silently truncate evidence.
func BuildEvidence(ctx context.Context, scan EntryScan, opt EvidenceOptions) (*support.Report, error) {
	var logs bytes.Buffer
	count := 0
	var writeErr error
	err := scan(ctx, func(e *Entry) bool {
		if ctx.Err() != nil {
			return false
		}
		if !opt.From.IsZero() || !opt.To.IsZero() {
			if e.Time.IsZero() || e.Event || !opt.From.IsZero() && e.Time.Before(opt.From) || !opt.To.IsZero() && e.Time.After(opt.To) {
				return true
			}
		}
		copy := *e
		if opt.Redact {
			copy.Raw = support.Redact(copy.Raw)
			copy.Message = support.Redact(copy.Message)
			copy.RawBytes = ""
			copy.Serial = "[设备]"
			copy.Tag = support.Redact(copy.Tag)
			copy.Package = support.Redact(copy.Package)
			copy.Process = support.Redact(copy.Process)
		}
		data, err := json.Marshal(copy)
		if err != nil {
			writeErr = err
			return false
		}
		if logs.Len()+len(data)+1 > 16<<20 || count >= 50000 {
			writeErr = fmt.Errorf("问题包日志超过 16 MiB 或 50000 条，请缩小时间范围或过滤条件")
			return false
		}
		logs.Write(data)
		logs.WriteByte('\n')
		count++
		return true
	})
	if err != nil {
		return nil, err
	}
	if writeErr != nil {
		return nil, writeErr
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("所选范围没有日志，请检查时间、过滤条件及来源")
	}
	info := map[string]string{}
	for k, v := range opt.Info {
		if opt.Redact {
			v = support.Redact(v)
		}
		info[k] = v
	}
	note, pkg, serial := opt.Note, opt.Package, opt.Serial
	if opt.Redact {
		note = support.Redact(note)
		pkg = support.Redact(pkg)
		serial = "[设备]"
	}
	meta := map[string]any{"created_at": time.Now(), "from": opt.From, "to_inclusive": opt.To, "scope": opt.Scope, "note": note, "package": pkg, "serial": serial, "device_info": info, "records": count, "redacted": opt.Redact, "limitations": "脱敏为文本规则尽力处理，图片未自动打码。设备时间可能回拨；实时缓存可能已有淘汰。截图和设备版本为收集当时的信息，不能证明历史状态。"}
	r := &support.Report{}
	if opt.Screenshot != nil {
		meta["screenshot_captured_at"] = opt.Screenshot.CapturedAt
		r.Files = append(r.Files, support.ReportFile{Name: "screenshot.png", Data: opt.Screenshot.PNG})
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	r.Files = append(r.Files, support.ReportFile{Name: "issue.json", Data: data}, support.ReportFile{Name: "logs.jsonl", Data: logs.Bytes()})
	return r, nil
}

func (a *ADB) EvidenceInfo(ctx context.Context, serial, pkg string) (map[string]string, error) {
	info := map[string]string{}
	for _, key := range []string{"ro.product.model", "ro.build.version.release", "ro.build.version.sdk"} {
		v, err := a.Run(ctx, serial, "shell", "getprop", key)
		if err != nil {
			return nil, err
		}
		info[key] = strings.TrimSpace(v)
	}
	if pkg != "" {
		if len(pkg) > 255 || strings.IndexFunc(pkg, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_')
		}) >= 0 {
			return nil, fmt.Errorf("应用包名格式不正确")
		}
		v, err := a.Run(ctx, serial, "shell", "dumpsys", "package", pkg)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(v, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "versionName=") || strings.HasPrefix(line, "versionCode=") {
				key, value, _ := strings.Cut(line, "=")
				info[key] = value
			}
		}
	}
	return info, nil
}

func EvidenceFilename() string {
	return "issue-" + time.Now().Format("20060102-150405.000000000") + ".zip"
}
func EvidencePath(dir string) string { return filepath.Join(dir, EvidenceFilename()) }
