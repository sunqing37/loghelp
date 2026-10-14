package core

import (
	"fmt"
	"sync"
)

type directoryBudget struct {
	sync.Mutex
	total int64
	users int
}

var budgets = struct {
	sync.Mutex
	items map[string]*directoryBudget
}{items: map[string]*directoryBudget{}}

func acquireBudget(root string) (*directoryBudget, error) {
	budgets.Lock()
	defer budgets.Unlock()
	budget := budgets.items[root]
	if budget == nil {
		budget = &directoryBudget{}
		budgets.items[root] = budget
	}
	budget.Lock()
	defer budget.Unlock()
	if budget.users == 0 {
		size, err := directorySize(root)
		if err != nil {
			return nil, err
		}
		budget.total = size
	}
	budget.users++
	return budget, nil
}
func (r *Recorder) reserve(size int64) error {
	b := r.budget
	b.Lock()
	defer b.Unlock()
	// Reserve across ALL recorders in this directory, including buffered writes.
	// A small margin covers manifests and rotation metadata.
	limit := r.options.QuotaBytes - 64<<10
	if limit < 0 {
		limit = r.options.QuotaBytes
	}
	if b.total+size > limit && r.options.Cleanup {
		physical, err := directorySize(r.options.Directory)
		if err != nil {
			return err
		}
		needed := b.total + size - limit
		target := physical - needed
		if target < 0 {
			target = 0
		}
		removed, err := cleanupManaged(r.options.Directory, r.options.RetentionDays, target)
		if err != nil {
			return err
		}
		b.total -= removed
	}
	if b.total+size > limit {
		return fmt.Errorf("目录容量已到上限；已停止录制，实时查看继续")
	}
	b.total += size
	return nil
}
func (r *Recorder) releaseBudget() {
	if r.budget != nil {
		r.budget.Lock()
		r.budget.users--
		r.budget.Unlock()
	}
}
