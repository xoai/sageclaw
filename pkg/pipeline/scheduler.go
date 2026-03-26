package pipeline

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// Lane identifies a scheduling lane.
type Lane string

const (
	LaneMain     Lane = "main"
	LaneSubagent Lane = "subagent"
	LaneDelegate Lane = "delegate"
	LaneCron     Lane = "cron"
)

// RunRequest represents a request to run the agent loop.
type RunRequest struct {
	SessionID string
	AgentID   string
	Messages  []canonical.Message
	Lane      Lane
	Priority  int
	HasAudio  bool // Hint: messages contain voice/audio content.
}

// Scheduler is the process boundary interface for scheduling (ADR-013).
type Scheduler interface {
	Schedule(ctx context.Context, lane Lane, req RunRequest) error
	Cancel(ctx context.Context, sessionKey string) error
	CancelAll(ctx context.Context, sessionKey string) error
}

// RunFunc is the function called to execute an agent run.
type RunFunc func(ctx context.Context, req RunRequest)

// LaneScheduler implements Scheduler with per-lane concurrency control.
type LaneScheduler struct {
	lanes   map[Lane]*laneQueue
	runFunc RunFunc
}

type laneQueue struct {
	sessions map[string]*sessionQueue
	sem      chan struct{}
	mu       sync.Mutex
}

type sessionQueue struct {
	queue      []RunRequest
	running    bool
	generation int64
	cancel     context.CancelFunc
}

// LaneLimits defines concurrency limits per lane.
type LaneLimits struct {
	Main     int
	Subagent int
	Delegate int
	Cron     int
}

// DefaultLaneLimits returns the default concurrency limits.
func DefaultLaneLimits() LaneLimits {
	return LaneLimits{Main: 1, Subagent: 2, Delegate: 3, Cron: 1}
}

// NewLaneScheduler creates a scheduler with per-lane concurrency.
func NewLaneScheduler(limits LaneLimits, runFunc RunFunc) *LaneScheduler {
	return &LaneScheduler{
		lanes: map[Lane]*laneQueue{
			LaneMain:     {sessions: make(map[string]*sessionQueue), sem: make(chan struct{}, limits.Main)},
			LaneSubagent: {sessions: make(map[string]*sessionQueue), sem: make(chan struct{}, limits.Subagent)},
			LaneDelegate: {sessions: make(map[string]*sessionQueue), sem: make(chan struct{}, limits.Delegate)},
			LaneCron:     {sessions: make(map[string]*sessionQueue), sem: make(chan struct{}, limits.Cron)},
		},
		runFunc: runFunc,
	}
}

var generationCounter int64

func (s *LaneScheduler) Schedule(ctx context.Context, lane Lane, req RunRequest) error {
	lq, ok := s.lanes[lane]
	if !ok {
		return fmt.Errorf("unknown lane: %s", lane)
	}

	lq.mu.Lock()
	sq, exists := lq.sessions[req.SessionID]
	if !exists {
		sq = &sessionQueue{}
		lq.sessions[req.SessionID] = sq
	}

	if sq.running {
		// Queue the request — per-session FIFO.
		sq.queue = append(sq.queue, req)
		lq.mu.Unlock()
		return nil
	}

	sq.running = true
	sq.generation = atomic.AddInt64(&generationCounter, 1)
	lq.mu.Unlock()

	// Run in goroutine.
	go func() {
		lq.sem <- struct{}{} // Acquire lane semaphore.
		defer func() { <-lq.sem }()

		s.runFunc(ctx, req)

		// Process queued requests.
		for {
			lq.mu.Lock()
			if len(sq.queue) == 0 {
				sq.running = false
				lq.mu.Unlock()
				return
			}
			next := sq.queue[0]
			sq.queue = sq.queue[1:]
			sq.generation = atomic.AddInt64(&generationCounter, 1)
			lq.mu.Unlock()

			s.runFunc(ctx, next)
		}
	}()

	return nil
}

func (s *LaneScheduler) Cancel(ctx context.Context, sessionKey string) error {
	// Cancel the current run for a session.
	for _, lq := range s.lanes {
		lq.mu.Lock()
		sq, exists := lq.sessions[sessionKey]
		if exists && sq.cancel != nil {
			sq.cancel()
		}
		lq.mu.Unlock()
	}
	return nil
}

func (s *LaneScheduler) CancelAll(ctx context.Context, sessionKey string) error {
	// Cancel and clear queue.
	for _, lq := range s.lanes {
		lq.mu.Lock()
		sq, exists := lq.sessions[sessionKey]
		if exists {
			sq.queue = nil
			if sq.cancel != nil {
				sq.cancel()
			}
		}
		lq.mu.Unlock()
	}
	return nil
}

// Compile-time check.
var _ Scheduler = (*LaneScheduler)(nil)
