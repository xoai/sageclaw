package team

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// TaskCompletion represents a completed task result awaiting lead consumption.
type TaskCompletion struct {
	TaskID    string
	AgentKey  string
	AgentName string
	Subject   string
	Result    string // Truncated to MaxResultLength.
	Status    string // "completed" or "failed"
	Error     string // If failed.
	BatchID   string
	Seq       int64
}

// MaxResultLength is the max chars stored per task result.
const MaxResultLength = 8000

// TeamInbox is a per-team queue for completed task results awaiting lead consumption.
type TeamInbox struct {
	mu          sync.Mutex
	completions []TaskCompletion
	seq         int64 // Monotonic counter for ordering.
}

// Push adds a completion to the inbox and returns its sequence number.
func (i *TeamInbox) Push(c TaskCompletion) int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	c.Seq = atomic.AddInt64(&i.seq, 1)
	i.completions = append(i.completions, c)
	return c.Seq
}

// ConsumeAll returns and clears all queued completions.
func (i *TeamInbox) ConsumeAll() []TaskCompletion {
	i.mu.Lock()
	defer i.mu.Unlock()
	result := i.completions
	i.completions = nil
	return result
}

// HasItems returns true if there are pending completions.
func (i *TeamInbox) HasItems() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.completions) > 0
}

// Len returns the number of pending completions.
func (i *TeamInbox) Len() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.completions)
}

// BatchComplete returns true if all tasks in the given batch are in the inbox.
// Used by progressive verbosity mode to wait for all tasks before waking lead.
func (i *TeamInbox) BatchComplete(batchID string, totalInBatch int) bool {
	if batchID == "" || totalInBatch <= 0 {
		return true
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	count := 0
	for _, c := range i.completions {
		if c.BatchID == batchID {
			count++
		}
	}
	return count >= totalInBatch
}

// TruncateResult truncates a result string to MaxResultLength.
func TruncateResult(s string) string {
	if len(s) <= MaxResultLength {
		return s
	}
	suffix := fmt.Sprintf("\n... (truncated, %d chars total)", len(s))
	cutAt := MaxResultLength - len(suffix)
	if cutAt < 0 {
		cutAt = 0
	}
	return s[:cutAt] + suffix
}
