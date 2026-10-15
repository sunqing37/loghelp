package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// Bookmark stores bounded metadata, never a reference into the live ring.
type Bookmark struct {
	ID          string    `json:"id"`
	Note        string    `json:"note"`
	Source      string    `json:"source,omitempty"`
	Paths       []string  `json:"paths,omitempty"`
	Seq         uint64    `json:"seq"`
	RecordedSeq uint64    `json:"recorded_seq,omitempty"`
	Serial      string    `json:"serial,omitempty"`
	Generation  uint64    `json:"generation,omitempty"`
	Time        time.Time `json:"time"`
	Received    time.Time `json:"received"`
	Digest      string    `json:"digest"`
	Preview     string    `json:"preview"`
}

func NewBookmark(e *Entry) Bookmark {
	return Bookmark{ID: fmt.Sprint(time.Now().UnixNano()), Seq: e.Seq, RecordedSeq: e.RecordedSeq,
		Serial: e.Serial, Generation: e.Generation, Time: e.Time, Received: e.Received,
		Digest: fmt.Sprintf("%x", sha256.Sum256([]byte(e.Raw))), Preview: strings.Clone(truncateUTF8(e.Raw, 512))}
}

func (b Bookmark) Matches(e *Entry, live bool) bool {
	if !e.Time.Equal(b.Time) {
		return false
	}
	if live {
		if e.Seq != b.Seq || e.Serial != b.Serial || e.Generation != b.Generation || !e.Received.Equal(b.Received) {
			return false
		}
	} else if e.RecordedSeq != 0 {
		seq := b.RecordedSeq
		if seq == 0 {
			seq = b.Seq
		}
		if e.RecordedSeq != seq || e.Serial != b.Serial || e.Generation != b.Generation || !e.Received.Equal(b.Received) {
			return false
		}
	} else if b.Source != "" && e.Seq != b.Seq {
		return false
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(e.Raw))) == b.Digest
}

func (o *Offline) FindBookmark(ctx context.Context, b Bookmark, progress func(int64)) (uint64, error) {
	var found uint64
	err := o.Scan(ctx, func(e *Entry) bool {
		if b.Matches(e, false) {
			found = e.Seq
			return false
		}
		return true
	}, progress)
	if err != nil {
		return 0, err
	}
	if found == 0 {
		return 0, fmt.Errorf("来源中没有书签日志，文件可能已变化，或该范围没有录制")
	}
	return found, nil
}
