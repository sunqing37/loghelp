// Package support keeps application diagnostics local. It never sends reports.
package support

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"loghelp"
)

const logLimit = 1 << 20

var sessionName = regexp.MustCompile(`^\d{8}T\d{6}Z-[a-f0-9]{12}$`)
var current atomic.Pointer[Session]

type record struct {
	Time     string `json:"time"`
	Level    string `json:"level"`
	Event    string `json:"event"`
	Detail   string `json:"detail,omitempty"`
	Version  string `json:"version"`
	Revision uint64 `json:"revision"`
}

type Session struct {
	mu              sync.Mutex
	queueMu         sync.Mutex
	queue           chan logRequest
	done            chan struct{}
	closing         bool
	Root, Directory string
	file            *os.File
	size            int64
	warning         atomic.Pointer[string]
	pendingCount    atomic.Int64
	pending         []string
	previous        io.Writer
	finished        bool
}

type logRequest struct {
	entry record
	flush chan error
}

// Start is called only by the primary application, after acquiring its instance
// lock and before loading settings or constructing UI. No panic is recovered:
// the Go runtime writes unhandled panics from every goroutine to crash.txt.
func Start() (*Session, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("获取诊断目录：%w", err)
	}
	root := filepath.Join(base, "loghelp", "diagnostics")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("创建诊断目录：%w", err)
	}
	var token [6]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, time.Now().UTC().Format("20060102T150405Z")+"-"+hex.EncodeToString(token[:]))
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	s := &Session{Root: root, Directory: dir, previous: log.Writer(), queue: make(chan logRequest, 256), done: make(chan struct{})}
	s.file, err = os.OpenFile(filepath.Join(dir, "application.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	go s.run()
	current.Store(s)
	log.SetOutput(s)
	if err := os.WriteFile(filepath.Join(dir, "running"), []byte(time.Now().UTC().Format(time.RFC3339)), 0600); err != nil {
		s.warn(fmt.Errorf("无法记录运行状态：%w", err))
	}
	crash, err := os.OpenFile(filepath.Join(dir, "crash.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = debug.SetCrashOutput(crash, debug.CrashOptions{})
		err = errors.Join(err, crash.Close())
	}
	if err != nil {
		s.warn(fmt.Errorf("无法启用崩溃记录：%w", err))
	}
	Record("INFO", "app.start", fmt.Sprintf("version=%s revision=%d os=%s arch=%s go=%s", loghelp.Version, loghelp.Revision, runtime.GOOS, runtime.GOARCH, runtime.Version()))
	if err := s.prune(); err != nil {
		s.warn(fmt.Errorf("诊断清理失败：%w", err))
	}
	dirs, err := s.sessions()
	if err != nil {
		s.warn(err)
	}
	for _, name := range dirs {
		if filepath.Join(root, name) == dir {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, name, "running.seen")); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			s.warn(err)
		}
		if _, err := os.Stat(filepath.Join(root, name, "running")); err == nil {
			s.pending = append(s.pending, name)
		} else if !os.IsNotExist(err) {
			s.warn(err)
		} else if info, err := os.Stat(filepath.Join(root, name, "crash.txt")); err == nil && info.Size() > 0 {
			// A teardown panic can happen after Finish removed the marker.
			s.pending = append(s.pending, name)
		}
	}
	s.pendingCount.Store(int64(len(s.pending)))
	return s, nil
}

func Record(level, event, detail string) {
	if s := current.Load(); s != nil {
		s.enqueue(level, event, detail)
	}
}

// Write adapts existing standard-library log calls without recording command
// arguments, device streams, environment variables or configuration snapshots.
func (s *Session) Write(p []byte) (int, error) {
	err := s.enqueue("INFO", "legacy.message", string(p))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Session) enqueue(level, event, detail string) error {
	if len(detail) > 16<<10 {
		detail = strings.Clone(detail[:16<<10]) + " [truncated]"
	}
	entry := record{Time: time.Now().UTC().Format(time.RFC3339Nano), Level: level, Event: event, Detail: detail, Version: loghelp.Version, Revision: loghelp.Revision}
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.closing {
		return nil
	}
	select {
	case s.queue <- logRequest{entry: entry}:
		return nil
	default:
		err := fmt.Errorf("运行日志队列已满，部分事件未保存；请检查磁盘性能或空间")
		s.warn(err)
		return err
	}
}

func (s *Session) run() {
	defer close(s.done)
	for request := range s.queue {
		if request.flush != nil {
			s.mu.Lock()
			err := s.fileSync()
			s.mu.Unlock()
			request.flush <- err
		} else {
			s.write(request.entry)
		}
	}
}

func (s *Session) fileSync() error {
	if s.file == nil {
		return fmt.Errorf("日志文件不可用")
	}
	return s.file.Sync()
}

func (s *Session) write(entry record) error {
	entry.Detail = Redact(entry.Detail)
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data = append(data, '\n')
	if s.size+int64(len(data)) > logLimit {
		err = s.rotate()
	}
	if err == nil && s.file != nil {
		var n int
		n, err = s.file.Write(data)
		s.size += int64(n)
		if err == nil && entry.Level == "ERROR" {
			err = s.file.Sync()
		}
	} else if err == nil {
		err = fmt.Errorf("日志文件不可用")
	}
	if err != nil {
		s.warnLocked(fmt.Errorf("运行日志写入失败：%w", err))
	}
	return err
}

func (s *Session) rotate() error {
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			return err
		}
		s.file = nil
	}
	base := filepath.Join(s.Directory, "application.jsonl")
	if err := os.Remove(base + ".2"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(base+".1", base+".2"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(base, base+".1"); err != nil {
		return err
	}
	var err error
	s.file, err = os.OpenFile(base, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	s.size = 0
	return err
}

func (s *Session) warn(err error) { s.warnLocked(err) }
func (s *Session) warnLocked(err error) {
	// Do not recurse into log or repeatedly fill stderr after a disk failure.
	message := err.Error()
	if s.warning.CompareAndSwap(nil, &message) {
		fmt.Fprintln(os.Stderr, message)
	}
}
func (s *Session) Warning() string {
	if s == nil {
		return "本次运行未启用本地诊断。"
	}
	if warning := s.warning.Load(); warning != nil {
		return *warning
	}
	return ""
}
func (s *Session) PendingCount() int {
	if s == nil {
		return 0
	}
	return int(s.pendingCount.Load())
}

// Acknowledge only clears notifications; it preserves logs and crash evidence.
func (s *Session) Acknowledge() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.pending) > 0 {
		path := filepath.Join(s.Root, s.pending[0], "running")
		if err := os.WriteFile(path+".seen", []byte(time.Now().UTC().Format(time.RFC3339)), 0600); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		s.pending = s.pending[1:]
		s.pendingCount.Store(int64(len(s.pending)))
	}
	return nil
}

// Finish must be called on an ordinary return, never with defer: a panic must
// leave the marker and crash descriptor intact until the runtime terminates.
func (s *Session) Finish(result error) {
	if s == nil {
		return
	}
	level, detail := "INFO", "正常退出"
	if result != nil {
		level, detail = "ERROR", result.Error()
	}
	Record(level, "app.exit", detail)
	s.queueMu.Lock()
	if !s.closing {
		s.closing = true
		close(s.queue)
	}
	s.queueMu.Unlock()
	<-s.done
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	if s.file != nil {
		if err := errors.Join(s.file.Sync(), s.file.Close()); err != nil {
			s.warnLocked(err)
		}
	}
	if result == nil {
		if err := os.Remove(filepath.Join(s.Directory, "running")); err != nil && !os.IsNotExist(err) {
			s.warnLocked(err)
		}
	}
	// Keep the runtime crash descriptor installed through process teardown.
	s.finished = true
	s.mu.Unlock()
	current.CompareAndSwap(s, nil)
	log.SetOutput(s.previous)
}

func (s *Session) sessions() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && sessionName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

func (s *Session) prune() error {
	names, err := s.sessions()
	if err != nil {
		return err
	}
	retained := 1
	for _, name := range names {
		if filepath.Join(s.Root, name) == s.Directory {
			continue
		}
		started, err := time.Parse("20060102T150405Z", strings.Split(name, "-")[0])
		if err != nil {
			continue
		}
		if retained < 20 && time.Since(started) < 14*24*time.Hour {
			retained++
			continue
		}
		// Only remove known files, never recursively delete an arbitrary directory.
		for _, file := range []string{"application.jsonl", "application.jsonl.1", "application.jsonl.2", "crash.txt", "running", "running.seen"} {
			if err := os.Remove(filepath.Join(s.Root, name, file)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := os.Remove(filepath.Join(s.Root, name)); err != nil {
			return err
		}
	}
	return nil
}
