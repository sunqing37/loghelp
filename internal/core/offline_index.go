package core

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Each source keeps at most two query indexes. Their checkpoints contain only
// positions and aggregates, never Entry pointers or retained log text.
type offlineQueryKey struct {
	Query, Search, Package string
	Sensitive              bool
}
type offlineQueryPoint struct {
	Offset, HitsBefore int
	Seq                uint64
	MaxTime            time.Time
}
type offlineQueryIndex struct {
	key                 offlineQueryKey
	Total, Hits, stride int
	points              []offlineQueryPoint
}

func (o *Offline) queryIndex(ctx context.Context, q Query, search Search, pkg string, progress func(int64)) (*offlineQueryIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, path := range o.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		original := o.files[i]
		if err != nil || !os.SameFile(info, original) || info.Size() < original.Size() || info.Size() == original.Size() && !info.ModTime().Equal(original.ModTime()) {
			return nil, fmt.Errorf("日志文件已改变或无法读取，请重新打开：%s", path)
		}
	}
	key := offlineQueryKey{Query: q.Text, Package: pkg, Sensitive: q.sensitive}
	if search.re != nil {
		key.Search = search.re.String()
	}
	o.cacheMu.Lock()
	for _, cached := range o.indexes {
		if cached.key == key {
			o.cacheMu.Unlock()
			return cached, nil
		}
	}
	o.cacheMu.Unlock()
	index := &offlineQueryIndex{key: key, stride: 512}
	if q.Text == "" && pkg == "" && search.re == nil {
		index.Total = o.count
		for _, p := range o.points {
			index.points = append(index.points, offlineQueryPoint{Offset: int(p.Seq - 1), Seq: p.Seq, MaxTime: p.MaxTime})
		}
	} else {
		err := o.Scan(ctx, func(e *Entry) bool {
			if !q.Match(e, o.Anchor) || pkg != "" && e.Package != pkg {
				return true
			}
			if index.Total%index.stride == 0 {
				index.points = append(index.points, offlineQueryPoint{Offset: index.Total, Seq: e.Seq, HitsBefore: index.Hits})
				if len(index.points) > maxOfflinePoints {
					for i := 0; i < len(index.points); i += 2 {
						p := index.points[i]
						if i+1 < len(index.points) && index.points[i+1].MaxTime.After(p.MaxTime) {
							p.MaxTime = index.points[i+1].MaxTime
						}
						index.points[i/2] = p
					}
					index.points = index.points[:(len(index.points)+1)/2]
					index.stride *= 2
				}
			}
			p := &index.points[len(index.points)-1]
			if e.Time.After(p.MaxTime) {
				p.MaxTime = e.Time
			}
			index.Total++
			index.Hits += len(search.Matches(e.Raw))
			return true
		}, progress)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.cacheMu.Lock()
	defer o.cacheMu.Unlock()
	if len(o.indexes) == 2 {
		o.indexes[0] = o.indexes[1]
		o.indexes = o.indexes[:1]
	}
	o.indexes = append(o.indexes, index)
	return index, nil
}

// Scan just one indexed result block. first/last are filtered result offsets;
// byte seeking uses the independent source index and preserves source Seq IDs.
func (o *Offline) scanQueryBlock(ctx context.Context, index *offlineQueryIndex, block int, q Query, pkg string, visit func(int, *Entry) bool, progress func(int64)) error {
	p := index.points[block]
	end := index.Total
	if block+1 < len(index.points) {
		end = index.points[block+1].Offset
	}
	current := p.Offset
	return o.scan(ctx, p.Seq, false, func(e *Entry) bool {
		if !q.Match(e, o.Anchor) || pkg != "" && e.Package != pkg {
			return true
		}
		keep := visit(current, e)
		current++
		return keep && current < end
	}, progress)
}

func (o *Offline) FindProgress(ctx context.Context, q Query, search Search, pkg string, after int, reverse bool, progress func(int64)) (int, error) {
	index, err := o.queryIndex(ctx, q, search, pkg, progress)
	if err != nil {
		return -1, err
	}
	start, end, step := 0, len(index.points), 1
	if reverse {
		start, end, step = len(index.points)-1, -1, -1
	}
	for i := start; i != end; i += step {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		p := index.points[i]
		last, hits := index.Total, index.Hits
		if i+1 < len(index.points) {
			last, hits = index.points[i+1].Offset, index.points[i+1].HitsBefore
		}
		if hits == p.HitsBefore || reverse && p.Offset >= after || !reverse && last <= after+1 {
			continue
		}
		found := -1
		err = o.scanQueryBlock(ctx, index, i, q, pkg, func(n int, e *Entry) bool {
			if reverse && n >= after {
				return false
			}
			if (reverse || n > after) && len(search.Matches(e.Raw)) > 0 {
				found = n
				return reverse
			}
			return true
		}, progress)
		if err != nil || found >= 0 {
			return found, err
		}
	}
	return -1, nil
}

// Block maxima allow clock reversals and mixed device/event timestamps: do
// not assume that the entire file is sorted by time.
func (o *Offline) FindTime(ctx context.Context, q Query, pkg string, target time.Time, progress func(int64)) (int, error) {
	index, err := o.queryIndex(ctx, q, Search{}, pkg, progress)
	if err != nil {
		return -1, err
	}
	for i, p := range index.points {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		if p.MaxTime.IsZero() || p.MaxTime.Before(target) {
			continue
		}
		found := -1
		err = o.scanQueryBlock(ctx, index, i, q, pkg, func(n int, e *Entry) bool {
			if !e.Time.IsZero() && !e.Time.Before(target) {
				found = n
				return false
			}
			return true
		}, progress)
		if err != nil || found >= 0 {
			return found, err
		}
	}
	return -1, nil
}

// FindRecorded maps a live entry to the offline ordinal, rather than assuming
// that a recording starts at live sequence 1. Generation and content prevent
// matching a different capture that reused the same sequence numbers.
func (o *Offline) FindRecorded(ctx context.Context, target *Entry, progress func(int64)) (uint64, error) {
	for i, p := range o.points {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if p.RecordedMax != 0 && (target.Seq < p.RecordedMin || target.Seq > p.RecordedMax) {
			continue
		}
		var found uint64
		end := uint64(o.count) + 1
		if i+1 < len(o.points) {
			end = o.points[i+1].Seq
		}
		err := o.scan(ctx, p.Seq, false, func(e *Entry) bool {
			if e.Seq >= end {
				return false
			}
			identity := e.RecordedSeq == 0 || e.RecordedSeq == target.Seq && e.Generation == target.Generation && e.Serial == target.Serial
			if identity && e.Raw == target.Raw && e.Time.Equal(target.Time) {
				found = e.Seq
				return false
			}
			return true
		}, progress)
		if err != nil {
			return 0, err
		}
		if found != 0 {
			return found, nil
		}
	}
	return 0, fmt.Errorf("录制文件中没有目标日志，可能未录制该范围、录制暂停或发生写入故障")
}
