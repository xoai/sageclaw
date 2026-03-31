package tool

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/xoai/sageclaw/pkg/store"
)

// pendingDispatchKey is the context key for the PendingDispatchQueue.
type pendingDispatchKey struct{}

// PendingDispatchQueue holds tasks that were created during a lead's tool call.
// Instead of dispatching immediately, tasks are queued and flushed after the
// lead's Run() returns. This ensures all tasks in a turn are created before
// any dispatch, so blocked_by references resolve correctly.
type PendingDispatchQueue struct {
	mu      sync.Mutex
	tasks   []store.TeamTask
	batchID string
}

// NewPendingDispatchQueue creates a new queue with a unique BatchID.
func NewPendingDispatchQueue() *PendingDispatchQueue {
	return &PendingDispatchQueue{
		batchID: uuid.NewString()[:8],
	}
}

// BatchID returns the queue's batch identifier. All tasks created in
// one turn share this ID.
func (q *PendingDispatchQueue) BatchID() string {
	return q.batchID
}

// Push adds a task to the pending queue.
func (q *PendingDispatchQueue) Push(task store.TeamTask) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
}

// Drain returns all queued tasks and empties the queue.
func (q *PendingDispatchQueue) Drain() []store.TeamTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	tasks := q.tasks
	q.tasks = nil
	return tasks
}

// Len returns the number of pending tasks.
func (q *PendingDispatchQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

// WithPendingDispatch adds a PendingDispatchQueue to the context.
func WithPendingDispatch(ctx context.Context, q *PendingDispatchQueue) context.Context {
	return context.WithValue(ctx, pendingDispatchKey{}, q)
}

// PendingDispatchFromCtx retrieves the PendingDispatchQueue from context, or nil.
func PendingDispatchFromCtx(ctx context.Context) *PendingDispatchQueue {
	q, _ := ctx.Value(pendingDispatchKey{}).(*PendingDispatchQueue)
	return q
}
