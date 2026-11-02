package orchestrator

import "sync"

// minCallTokens is the smallest max_tokens worth sending; below it a call
// would likely be cut off mid-answer.
const minCallTokens = 2048

// budget enforces a run's output-token limit across concurrent model calls.
// A call reserves its max_tokens up front and settles with actual usage.
type budget struct {
	mu       sync.Mutex
	limit    int64
	used     int64
	reserved int64
}

func newBudget(limit int64) *budget { return &budget{limit: limit} }

// reserve grants up to want tokens, or 0 if fewer than minCallTokens remain.
func (b *budget) reserve(want int64) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	avail := b.limit - b.used - b.reserved
	if avail < minCallTokens {
		return 0
	}
	grant := min(want, avail)
	b.reserved += grant
	return grant
}

func (b *budget) settle(granted, actual int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= granted
	b.used += actual
}

func (b *budget) spent() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
