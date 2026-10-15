package core

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Entries are immutable after publication so views can share them safely.
type Entry struct {
	Seq         uint64    `json:"seq"`
	RecordedSeq uint64    `json:"-"`
	Time        time.Time `json:"time,omitempty"`
	Received    time.Time `json:"received"`
	Serial      string    `json:"serial,omitempty"`
	Generation  uint64    `json:"generation,omitempty"`
	Buffer      string    `json:"buffer,omitempty"`
	PID         int       `json:"pid,omitempty"`
	TID         int       `json:"tid,omitempty"`
	Level       string    `json:"level,omitempty"`
	Tag         string    `json:"tag,omitempty"`
	Package     string    `json:"package,omitempty"`
	Process     string    `json:"process,omitempty"`
	Message     string    `json:"message"`
	Raw         string    `json:"raw"`
	RawBytes    string    `json:"raw_bytes,omitempty"`
	Event       bool      `json:"event,omitempty"`
}

var threadLine = regexp.MustCompile(`^(?:(\d{4})-)?(\d{2}-\d{2})\s+(\d{2}:\d{2}:\d{2}\.\d+)\s+(\d+)\s+(\d+)\s+([VDIWEAF])\s+(.+?)\s*:\s?(.*)$`)
var epochLine = regexp.MustCompile(`^\s*(\d{9,}\.\d+)\s+(\d+)\s+(\d+)\s+([VDIWEAF])\s+(.+?)\s*:\s?(.*)$`)

func ParseLine(raw string, reference time.Time) *Entry {
	e := &Entry{Received: time.Now(), Raw: raw, Message: raw}
	if !utf8.ValidString(raw) {
		e.RawBytes = base64.StdEncoding.EncodeToString([]byte(raw))
		e.Raw = strings.ToValidUTF8(raw, "�")
		e.Message = e.Raw
	}
	if m := epochLine.FindStringSubmatch(e.Raw); m != nil {
		parts := strings.SplitN(m[1], ".", 2)
		seconds, _ := strconv.ParseInt(parts[0], 10, 64)
		nanos, _ := strconv.ParseInt((parts[1] + "000000000")[:9], 10, 64)
		e.Time = time.Unix(seconds, nanos)
		e.PID, _ = strconv.Atoi(m[2])
		e.TID, _ = strconv.Atoi(m[3])
		e.Level, e.Tag, e.Message = m[4], strings.TrimSpace(m[5]), m[6]
		return e
	}
	if m := threadLine.FindStringSubmatch(e.Raw); m != nil {
		year := reference.Year()
		if m[1] != "" {
			year, _ = strconv.Atoi(m[1])
		}
		e.Time, _ = time.ParseInLocation("2006-01-02 15:04:05.999999999", fmt.Sprintf("%04d-%s %s", year, m[2], m[3]), reference.Location())
		if m[1] == "" && e.Time.After(reference.AddDate(0, 6, 0)) {
			e.Time = e.Time.AddDate(-1, 0, 0)
		}
		e.PID, _ = strconv.Atoi(m[4])
		e.TID, _ = strconv.Atoi(m[5])
		e.Level, e.Tag, e.Message = m[6], strings.TrimSpace(m[7]), m[8]
	}
	return e
}

func (e *Entry) Size() int64 {
	return int64(240 + len(e.Raw) + len(e.RawBytes) + len(e.Message) + len(e.Tag) + len(e.Package) + len(e.Process))
}

func LevelValue(s string) int {
	switch strings.ToUpper(s) {
	case "V", "VERBOSE":
		return 1
	case "D", "DEBUG":
		return 2
	case "I", "INFO":
		return 3
	case "W", "WARN", "WARNING":
		return 4
	case "E", "ERROR":
		return 5
	case "A", "F", "ASSERT", "FATAL":
		return 6
	}
	return 0
}

type Ring struct {
	mu           sync.RWMutex
	items        []*Entry
	head, count  int
	bytes, limit int64
	seq, dropped uint64
}

func NewRing(count int, bytes int64) *Ring {
	if count < 1 {
		count = 200000
	}
	if bytes < 1 {
		bytes = 64 << 20
	}
	return &Ring{items: make([]*Entry, count), limit: bytes}
}

func (r *Ring) Add(e *Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	e.Seq = r.seq
	for r.count > 0 && (r.count == len(r.items) || r.bytes+e.Size() > r.limit) {
		old := r.items[r.head]
		r.bytes -= old.Size()
		r.items[r.head] = nil
		r.head = (r.head + 1) % len(r.items)
		r.count--
		r.dropped++
	}
	// Oversized entries remain in recordings; the display gets a bounded preview.
	view := e
	if e.Size() > r.limit {
		copy := *e
		n := int(r.limit / 4)
		if n < 1 {
			n = 1
		}
		copy.Raw = truncateUTF8(copy.Raw, n)
		copy.Message = truncateUTF8(copy.Message, n)
		copy.RawBytes = ""
		copy.Message += " [显示截断；完整内容在录制中]"
		view = &copy
	}
	r.items[(r.head+r.count)%len(r.items)] = view
	r.count++
	r.bytes += view.Size()
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (r *Ring) Snapshot() ([]*Entry, uint64, uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, r.count)
	for i := range out {
		out[i] = r.items[(r.head+i)%len(r.items)]
	}
	return out, r.seq, r.dropped
}

func (r *Ring) Version() uint64 { r.mu.RLock(); defer r.mu.RUnlock(); return r.seq }
