package support

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"loghelp"
)

type Feedback struct {
	Summary, Steps, Expected, Actual string
	Screenshots                      []Screenshot
}
type ReportFile struct {
	Name string
	Data []byte
}
type Report struct {
	Files       []ReportFile
	Screenshots map[string]Screenshot
}

var eventCode = regexp.MustCompile(`^[a-z]+(?:\.[a-z_]+)+$`)
var versionCode = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)
var functionName = regexp.MustCompile(`^[A-Za-z0-9_./*()\[\],-]+$`)
var sourceLine = regexp.MustCompile(`(?:^|[/\\])([A-Za-z0-9_-]+\.go):([0-9]+)`)

// BuildReport freezes exactly the bytes later previewed and saved. Raw log
// details, crash messages, stack argument values and source paths never enter it.
func (s *Session) BuildReport(ctx context.Context, feedback Feedback) (*Report, error) {
	r := &Report{}
	if len(feedback.Screenshots) > MaxScreenshots {
		return nil, fmt.Errorf("最多添加 %d 张截图", MaxScreenshots)
	}
	r.Screenshots = make(map[string]Screenshot)
	for index, shot := range feedback.Screenshots {
		if len(shot.PNG) == 0 || len(shot.PNG) > screenshotLimit || shot.Thumbnail == nil {
			return nil, fmt.Errorf("截图附件无效，请重新选择")
		}
		name := fmt.Sprintf("screenshots/screenshot-%02d.png", index+1)
		r.Files = append(r.Files, ReportFile{name, shot.PNG})
		r.Screenshots[name] = shot
	}
	environment, _ := json.MarshalIndent(struct {
		Version  string `json:"version"`
		Revision uint64 `json:"revision"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Go       string `json:"go"`
		Created  string `json:"created_utc"`
	}{loghelp.Version, loghelp.Revision, runtime.GOOS, runtime.GOARCH, runtime.Version(), time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	r.Files = append(r.Files, ReportFile{"environment.json", environment})
	var text strings.Builder
	text.WriteString("# LogHelp 问题反馈\n\n")
	for _, field := range []struct{ name, value string }{{"问题概述", feedback.Summary}, {"复现步骤", feedback.Steps}, {"预期结果", feedback.Expected}, {"实际结果", feedback.Actual}} {
		if len(field.value) > 4096 {
			return nil, fmt.Errorf("%s超过 4096 字节，请精简后重试", field.name)
		}
		fmt.Fprintf(&text, "## %s\n\n%s\n\n", field.name, Redact(field.value))
	}
	r.Files = append(r.Files, ReportFile{"feedback.md", []byte(text.String())})
	readme := "这是用户主动导出的本地诊断包；LogHelp 不上传任何文件。\n" +
		"环境信息仅含应用版本、修订号、系统、架构、Go 版本和导出时间。\n" +
		"自动日志仅保留时间、级别、事件代码和该会话的应用版本/修订，所有 detail 字段均已丢弃。\n" +
		"崩溃摘要只保留允许的函数名、文件名、行号；不含原始 panic 文本、参数或完整路径。\n" +
		"不自动收集 Android 日志、录制、截图、配置、设备标识、Shell 历史或环境变量。\n" +
		"screenshots/ 只含用户主动选择的截图，统一转为 PNG 并移除原文件名和元数据；图片中的文字/像素不会自动打码。分享前请人工检查。\n" +
		"问题描述经过常见路径、网址、IP、邮箱和凭据脱敏，但任意自然语言无法保证完全脱敏，请分享前检查。\n" +
		"最多包含当前会话及最近四个历史会话（优先未确认的异常会话），每份日志只读取末尾 128 KiB，崩溃只读取开头 512 KiB；摘要可能不完整。\n" +
		"running 表示没有记录正常退出，也可能由强制结束、断电或错误返回造成，不能单独证明发生 panic。\n"
	if s == nil {
		readme += "本次运行未启用日志记录，包内仅含环境信息和用户反馈。\n"
	} else {
		if s.Warning() != "" {
			readme += "本次诊断组件报告过故障，日志可能不完整；请在应用内查看具体提示。\n"
		}
		if err := s.flush(ctx); err != nil {
			return nil, fmt.Errorf("刷新诊断日志：%w", err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		names, err := s.sessions()
		if err != nil {
			return nil, err
		}
		selected := []string{filepath.Base(s.Directory)}
		for _, list := range [][]string{s.pending, names} {
			for _, name := range list {
				found := false
				for _, existing := range selected {
					if existing == name {
						found = true
						break
					}
				}
				if !found && len(selected) < 5 {
					selected = append(selected, name)
				}
			}
		}
		for index, name := range selected {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prefix := fmt.Sprintf("session-%d/", index+1)
			base := filepath.Join(s.Root, name)
			for _, filename := range []string{"application.jsonl.2", "application.jsonl.1", "application.jsonl", "crash.txt"} {
				limit, tail := int64(128<<10), true
				if filename == "crash.txt" {
					limit, tail = 512<<10, false
				}
				data, truncated, err := readBounded(filepath.Join(base, filename), limit, tail)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return nil, fmt.Errorf("读取诊断文件 %s：%w", filename, err)
				}
				if truncated {
					readme += prefix + filename + "：已截断。\n"
				}
				if filename == "crash.txt" {
					if len(data) == 0 {
						continue
					}
					data = crashSummary(data)
					filename = "crash-summary.txt"
				} else {
					data = eventSummary(data)
				}
				r.Files = append(r.Files, ReportFile{prefix + filename, data})
			}
			state := "已结束或通知已确认"
			if index == 0 {
				state = "当前运行会话"
			} else if _, err := os.Stat(filepath.Join(base, "running")); err == nil {
				state = "未记录正常退出；通知未确认"
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			r.Files = append(r.Files, ReportFile{prefix + "state.txt", []byte(state)})
		}
	}
	r.Files = append(r.Files, ReportFile{"README.txt", []byte(readme)})
	return r, ctx.Err()
}

func (s *Session) flush(ctx context.Context) error {
	result := make(chan error, 1)
	s.queueMu.Lock()
	if s.closing {
		s.queueMu.Unlock()
		return fmt.Errorf("程序正在退出，请下次启动后导出")
	}
	select {
	case s.queue <- logRequest{flush: result}:
		s.queueMu.Unlock()
	default:
		s.queueMu.Unlock()
		return fmt.Errorf("日志正在写入，请稍后重试")
	}
	select {
	case err := <-result:
		if err != nil {
			s.warn(err)
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func readBounded(path string, limit int64, tail bool) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("诊断文件不是普通文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	truncated := info.Size() > limit
	if tail && truncated {
		if _, err := f.Seek(-limit, io.SeekEnd); err != nil {
			return nil, false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if tail && truncated {
		if end := bytes.IndexByte(data, '\n'); end >= 0 {
			data = data[end+1:]
		} else {
			data = nil
		}
	}
	return data, truncated, err
}

func eventSummary(data []byte) []byte {
	var output bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 32<<10), 256<<10)
	for scanner.Scan() {
		var entry record
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || !eventCode.MatchString(entry.Event) || len(entry.Event) > 64 {
			continue
		}
		if entry.Level != "INFO" && entry.Level != "WARN" && entry.Level != "ERROR" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, entry.Time); err != nil {
			continue
		}
		entry.Detail = ""
		if !versionCode.MatchString(entry.Version) || len(entry.Version) > 64 {
			entry.Version = "unknown"
		}
		_ = json.NewEncoder(&output).Encode(entry)
	}
	return output.Bytes()
}

func crashSummary(data []byte) []byte {
	var output strings.Builder
	output.WriteString("检测到 Go 崩溃输出。原始异常文本、参数和完整路径已排除。\n")
	lines := strings.Split(string(data), "\n")
	for index, line := range lines {
		if index+1 >= len(lines) {
			break
		}
		if !strings.HasPrefix(line, "loghelp/") && !strings.HasPrefix(line, "main.") && !strings.HasPrefix(line, "runtime.") && !strings.HasPrefix(line, "github.com/") && !strings.HasPrefix(line, "golang.org/") {
			continue
		}
		end := strings.LastIndex(line, "(")
		if end <= 0 {
			continue
		}
		name := line[:end]
		if !functionName.MatchString(name) || len(name) > 240 {
			continue
		}
		location := sourceLine.FindStringSubmatch(lines[index+1])
		if len(location) == 3 {
			fmt.Fprintf(&output, "%s\n  %s:%s\n", name, location[1], location[2])
		}
	}
	return []byte(output.String())
}

// Save writes a new file exclusively: never truncate a user file or follow a
// destination symlink. A failed export removes only this newly-created file.
func (r *Report) Save(ctx context.Context, path string) (result error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("创建诊断包失败（请使用新的文件名）：%w", err)
	}
	defer func() {
		if err := f.Close(); result == nil {
			result = err
		}
		if result != nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				result = errors.Join(result, fmt.Errorf("清理未完成的诊断包：%w", err))
			}
		}
	}()
	z := zip.NewWriter(f)
	for _, file := range r.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := z.Create(file.Name)
		if err != nil {
			return err
		}
		if _, err := entry.Write(file.Data); err != nil {
			return err
		}
	}
	if err := z.Close(); err != nil {
		return err
	}
	return f.Sync()
}
