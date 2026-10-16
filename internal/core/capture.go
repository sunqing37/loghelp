package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"loghelp/internal/support"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Collector struct {
	ADB           *ADB
	Serial        string
	Buffers       []string
	Ring          *Ring
	mu            sync.RWMutex
	emitMu        sync.Mutex
	status        string
	processes     map[int]Process
	packages      map[string]bool
	clockSkew     time.Duration
	info          map[string]string
	starting      bool
	ready         bool
	generation    uint64
	refs          int
	Recorder      *Recorder
	recordings    []string
	keepRecording bool
	recordJob     uint64
	cancel        context.CancelFunc
	done          chan struct{}
}
type Hub struct {
	mu     sync.Mutex
	items  map[string]*Collector
	Config Config
	ADB    *ADB
}

func NewHub(a *ADB, c Config) *Hub { return &Hub{items: map[string]*Collector{}, ADB: a, Config: c} }
func captureKey(serial string, buffers []string) string {
	b := append([]string(nil), buffers...)
	sort.Strings(b)
	return serial + "/" + strings.Join(b, ",")
}
func (h *Hub) Acquire(serial string, buffers []string) *Collector {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := captureKey(serial, buffers)
	if c := h.items[key]; c != nil {
		c.refs++
		return c
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Collector{ADB: h.ADB, Serial: serial, Buffers: append([]string(nil), buffers...), Ring: NewRing(h.Config.CacheCount, h.Config.CacheBytes), processes: map[int]Process{}, refs: 1, cancel: cancel, done: make(chan struct{})}
	h.items[key] = c
	go c.run(ctx)
	return c
}
func (h *Hub) Release(c *Collector) {
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.refs > 0 {
		c.refs--
	}
	h.prune(c)
}
func (h *Hub) prune(c *Collector) {
	if h.items[captureKey(c.Serial, c.Buffers)] != c {
		return
	}
	if c.refs == 0 && !c.retainRecording() {
		delete(h.items, captureKey(c.Serial, c.Buffers))
		c.cancel()
	}
}
func (h *Hub) Items() []*Collector {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*Collector
	for _, c := range h.items {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Serial < out[j].Serial })
	return out
}
func (h *Hub) StartRecording(c *Collector, options RecordOptions) error {
	return h.startRecording(c, options, "")
}

func (h *Hub) RecoverRecording(c *Collector, options RecordOptions, failedDirectory string) error {
	if failedDirectory == "" {
		return fmt.Errorf("没有可恢复的故障录制")
	}
	return h.startRecording(c, options, failedDirectory)
}

func (h *Hub) startRecording(c *Collector, options RecordOptions, failedDirectory string) error {
	h.mu.Lock()
	if h.items[captureKey(c.Serial, c.Buffers)] != c {
		h.mu.Unlock()
		return fmt.Errorf("采集流已关闭，请重新开始采集后录制")
	}
	c.mu.Lock()
	if failedDirectory != "" && (c.Recorder == nil || c.Recorder.Status().Directory != failedDirectory || c.Recorder.Status().Error == "") {
		c.mu.Unlock()
		h.mu.Unlock()
		return fmt.Errorf("故障录制已改变，请刷新录制管理后重试")
	}
	if !c.ready {
		c.mu.Unlock()
		h.mu.Unlock()
		return fmt.Errorf("设备尚未完成连接验证，请等待采集启动后录制")
	}
	if c.starting || c.Recorder != nil && c.Recorder.Status().Active {
		c.mu.Unlock()
		h.mu.Unlock()
		return fmt.Errorf("该采集流已经在录制")
	}
	c.starting = true
	c.recordJob++
	job := c.recordJob
	c.refs++
	previous := c.Recorder
	options.DeviceInfo = make(map[string]string, len(c.info))
	for key, value := range c.info {
		options.DeviceInfo[key] = value
	}
	c.mu.Unlock()
	h.mu.Unlock()
	// A failed writer may still be flushing its bounded queue. Wait for its
	// final durable boundary before creating the replacement in another root.
	if previous != nil {
		_ = previous.Stop() // The old fault is preserved below, not a new failure.
		status := previous.Status()
		if status.Error != "" {
			options.Recovery = &RecordingRecovery{PreviousDirectory: status.Directory, LastSeq: status.LastSeq, Unwritten: status.Unwritten, LastReceived: status.LastReceived, FailedAt: status.FailedAt, Reason: status.Error}
		}
	}
	r, err := NewRecorder(c.Serial, c.Buffers, options)
	h.mu.Lock()
	c.emitMu.Lock()
	c.mu.Lock()
	c.starting = false
	c.refs--
	valid := h.items[captureKey(c.Serial, c.Buffers)] == c && c.recordJob == job
	if err == nil && valid {
		c.Recorder = r
		c.keepRecording = true
		c.recordings = append(c.recordings, r.Status().Directory)
		if len(c.recordings) > 64 {
			c.recordings = append([]string(nil), c.recordings[len(c.recordings)-64:]...)
		}
	}
	c.mu.Unlock()
	if err == nil && valid && options.Recovery != nil {
		now := time.Now()
		message := fmt.Sprintf("录制恢复边界：上一会话 %s，最后持久化序号 %d；从本条之后继续写入，期间日志未回填，可能存在缺口。", options.Recovery.PreviousDirectory, options.Recovery.LastSeq)
		c.mu.RLock()
		generation := c.generation
		c.mu.RUnlock()
		entry := &Entry{Time: now, Received: now, Serial: c.Serial, Buffer: strings.Join(c.Buffers, ","), Generation: generation, Event: true, Level: "W", Tag: "LogHelp", Raw: message, Message: message}
		c.Ring.Add(entry)
		r.Append(entry)
	}
	c.emitMu.Unlock()
	h.prune(c)
	h.mu.Unlock()
	if !valid && r != nil {
		r.Stop()
		return fmt.Errorf("采集会话已关闭")
	}
	return err
}

func (h *Hub) UpdateConfig(config Config) { h.mu.Lock(); h.Config = config; h.mu.Unlock() }
func (h *Hub) StopRecording(c *Collector) error {
	c.mu.Lock()
	r := c.Recorder
	c.keepRecording = false
	c.recordJob++
	c.mu.Unlock()
	var err error
	if r != nil {
		err = r.Stop()
	}
	h.mu.Lock()
	h.prune(c)
	h.mu.Unlock()
	return err
}
func (h *Hub) Close() {
	h.mu.Lock()
	items := h.items
	h.items = map[string]*Collector{}
	h.mu.Unlock()
	for _, c := range items {
		c.cancel()
	}
	for _, c := range items {
		<-c.done
		c.mu.RLock()
		r := c.Recorder
		c.mu.RUnlock()
		if r != nil {
			r.Stop()
		}
	}
}
func (c *Collector) Status() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.status }
func (c *Collector) IsRecording() bool {
	c.mu.RLock()
	r := c.Recorder
	c.mu.RUnlock()
	return r != nil && r.Status().Active
}
func (c *Collector) retainRecording() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.starting {
		return true
	}
	if c.Recorder == nil {
		return false
	}
	status := c.Recorder.Status()
	return status.Active || c.keepRecording && status.Error != ""
}

func (c *Collector) RecordingDirectories() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.recordings...)
}
func (c *Collector) RecordingStatus() RecordStatus {
	c.mu.RLock()
	r := c.Recorder
	c.mu.RUnlock()
	if r != nil {
		return r.Status()
	}
	return RecordStatus{}
}
func (c *Collector) PauseRecording(pause bool) {
	c.mu.RLock()
	r := c.Recorder
	c.mu.RUnlock()
	if r != nil {
		r.Pause(pause)
	}
}
func (c *Collector) emit(e *Entry) {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	e.Serial = c.Serial
	e.Buffer = strings.Join(c.Buffers, ",")
	c.mu.RLock()
	e.Generation = c.generation
	proc := c.processes[e.PID]
	skew := c.clockSkew
	recorder := c.Recorder
	c.mu.RUnlock()
	if !e.Time.IsZero() && !e.Time.Add(skew).Before(proc.FirstSeen.Add(-time.Second)) {
		e.Process, e.Package = proc.Name, proc.Package
	}
	c.Ring.Add(e)
	if recorder != nil {
		recorder.Append(e)
	}
}
func (c *Collector) event(message string) {
	c.mu.Lock()
	c.status = message
	c.mu.Unlock()
	c.emit(&Entry{Time: time.Now(), Received: time.Now(), Tag: "LogHelp", Level: "I", Message: message, Raw: message, Event: true})
}
func (c *Collector) updateProcesses(ctx context.Context) {
	lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	processes, err := c.ADB.Processes(lookup, c.Serial)
	c.mu.Lock()
	var events []string
	if err != nil {
		c.processes = map[int]Process{}
	} else {
		now := time.Now()
		for pid, proc := range processes {
			proc.FirstSeen = now
			if old, ok := c.processes[pid]; ok && old.Name == proc.Name {
				proc.FirstSeen = old.FirstSeen
			}
			if !c.packages[proc.Package] {
				proc.Package = ""
			}
			processes[pid] = proc
		}
		if len(c.processes) > 0 {
			for pid, old := range c.processes {
				current, ok := processes[pid]
				if old.Package != "" && (!ok || old.Name != current.Name) {
					events = append(events, fmt.Sprintf("进程结束：%s (%d)", old.Name, pid))
				}
			}
			for pid, current := range processes {
				old, ok := c.processes[pid]
				if current.Package != "" && (!ok || old.Name != current.Name) {
					events = append(events, fmt.Sprintf("进程启动：%s (%d)", current.Name, pid))
				}
			}
		}
		c.processes = processes
	}
	c.mu.Unlock()
	for _, event := range events {
		c.emit(&Entry{Time: time.Now(), Received: time.Now(), Level: "I", Tag: "LogHelp", Message: event, Raw: event, Event: true})
	}
}
func (c *Collector) run(ctx context.Context) {
	defer close(c.done)
	backoff := time.Second
	var last time.Time
	for ctx.Err() == nil {
		c.mu.Lock()
		c.generation++
		c.ready = false
		c.processes = map[int]Process{}
		c.mu.Unlock()
		check, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := c.ADB.CheckBuffers(check, c.Serial, c.Buffers)
		cancel()
		if err == nil {
			lookup, lookupCancel := context.WithTimeout(ctx, 8*time.Second)
			packages, _ := c.ADB.Packages(lookup, c.Serial)
			clock, _ := c.ADB.Run(lookup, c.Serial, "shell", "date", "+%s")
			lookupCancel()
			c.mu.Lock()
			c.packages = map[string]bool{}
			for _, pkg := range packages {
				c.packages[pkg] = true
			}
			c.mu.Unlock()
			if seconds, parseErr := strconv.ParseInt(strings.TrimSpace(clock), 10, 64); parseErr == nil {
				skew := time.Since(time.Unix(seconds, 0))
				c.mu.Lock()
				c.clockSkew = skew
				c.mu.Unlock()
				if skew > 2*time.Minute || skew < -2*time.Minute {
					c.event(fmt.Sprintf("设备与主机时钟相差 %s；age 筛选可能受影响", skew.Round(time.Second)))
				}
			}
			infoCtx, infoCancel := context.WithTimeout(ctx, 5*time.Second)
			props, _ := c.ADB.Run(infoCtx, c.Serial, "shell", "getprop")
			infoCancel()
			info := map[string]string{}
			for _, line := range strings.Split(props, "\n") {
				line = strings.TrimSuffix(line, "\r")
				for _, key := range []string{"ro.product.model", "ro.product.manufacturer", "ro.build.version.sdk", "ro.build.version.release", "persist.sys.timezone"} {
					prefix := "[" + key + "]: ["
					if strings.HasPrefix(line, prefix) {
						info[key] = strings.TrimSuffix(strings.TrimPrefix(line, prefix), "]")
					}
				}
			}
			c.mu.Lock()
			c.info = info
			c.mu.Unlock()
			c.updateProcesses(ctx)
			c.event("连接已验证，正在启动日志流")
			runCtx, runCancel := context.WithCancel(ctx)
			args := append([]string{"logcat", "-v", "epoch"}, BufferArgs(c.Buffers)...)
			if !last.IsZero() {
				args = append(args, "-T", last.Local().Format("01-02 15:04:05.000"))
				c.event("连接恢复：尝试按时间续采，边界可能重复，缓冲区覆盖或重启可能造成缺口")
			}
			cmd := c.ADB.Command(runCtx, c.Serial, args...)
			stdout, pipeErr := cmd.StdoutPipe()
			stderr := &limitedBuffer{limit: 8192}
			cmd.Stderr = stderr
			if pipeErr == nil {
				pipeErr = cmd.Start()
			}
			if pipeErr == nil {
				c.mu.Lock()
				c.ready = true
				c.mu.Unlock()
				c.event("采集中 · " + strings.Join(c.Buffers, ","))
				pollDone := make(chan struct{})
				go func() {
					defer close(pollDone)
					ticker := time.NewTicker(2 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-runCtx.Done():
							return
						case <-ticker.C:
							c.updateProcesses(runCtx)
						}
					}
				}()
				readErr := readLines(stdout, func(raw string) {
					e := ParseLine(raw, time.Now())
					if !e.Time.IsZero() {
						last = e.Time
					}
					c.emit(e)
				})
				if readErr != nil {
					runCancel()
				}
				err = cmd.Wait()
				if err == nil && readErr != nil {
					err = readErr
				}
				runCancel()
				<-pollDone
				if stderr.b.Len() > 0 {
					err = fmt.Errorf("%v：%s", err, stderr.b.String())
				}
			} else {
				err = pipeErr
				runCancel()
			}
		}
		if ctx.Err() != nil {
			break
		}
		c.mu.Lock()
		c.ready = false
		c.mu.Unlock()
		if err == nil {
			err = fmt.Errorf("日志流结束")
		}
		c.event("采集中断，将重试：" + err.Error())
		support.Record("WARN", "capture.retry", err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff *= 2
			if backoff > 15*time.Second {
				backoff = 15 * time.Second
			}
		}
	}
}

func readStreamLines(r io.Reader, consume func(string)) error {
	reader := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err, truncated := boundedLine(reader)
		if truncated {
			return fmt.Errorf("日志单条超过 16 MiB，采集已中断并标记缺口")
		}
		if len(line) > 0 {
			consume(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
