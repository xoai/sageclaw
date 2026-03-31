package typing

import (
	"context"
	"log"
	"sync"
	"time"
)

// Controller manages typing indicator lifecycle for a single session.
// It provides keepalive re-sends and safety timeouts.
type Controller struct {
	mu            sync.Mutex
	startFn       func() error // Send typing indicator
	stopFn        func() error // Clear indicator (may be nil)
	keepaliveMs   int
	maxDurationMs int

	running      bool
	runComplete  bool // Signal 1: agent finished
	dispatchIdle bool // Signal 2: messages delivered

	cancelKeep context.CancelFunc
	cancelTTL  context.CancelFunc
	stopped    bool // prevents post-close startFn calls
}

// NewController creates a typing controller.
// startFn sends the typing indicator. stopFn clears it (can be nil).
// keepaliveMs is the re-send interval. maxDurationMs is the TTL safety net.
func NewController(startFn, stopFn func() error, keepaliveMs, maxDurationMs int) *Controller {
	return &Controller{
		startFn:       startFn,
		stopFn:        stopFn,
		keepaliveMs:   keepaliveMs,
		maxDurationMs: maxDurationMs,
	}
}

// Start begins the typing indicator and keepalive loop.
func (c *Controller) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running || c.stopped {
		return
	}
	c.running = true
	c.runComplete = false
	c.dispatchIdle = false

	// Send initial typing indicator.
	c.callStartFn()

	// Keepalive goroutine.
	keepCtx, keepCancel := context.WithCancel(context.Background())
	c.cancelKeep = keepCancel
	go c.keepaliveLoop(keepCtx)

	// TTL safety goroutine.
	ttlCtx, ttlCancel := context.WithCancel(context.Background())
	c.cancelTTL = ttlCancel
	go c.ttlLoop(ttlCtx)
}

// MarkRunComplete signals that the agent run has finished.
// The indicator persists until MarkDispatchIdle is also called (or TTL expires).
func (c *Controller) MarkRunComplete() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runComplete = true
	c.tryCleanup()
}

// MarkDispatchIdle signals that all messages have been delivered to the channel.
func (c *Controller) MarkDispatchIdle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dispatchIdle = true
	c.tryCleanup()
}

// Stop force-stops the typing indicator. Idempotent.
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

// ExtendTTL extends the TTL safety net (e.g., for long-running delegations).
func (c *Controller) ExtendTTL(durationMs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stopped {
		return
	}
	// Cancel current TTL and restart with new duration.
	if c.cancelTTL != nil {
		c.cancelTTL()
	}
	c.maxDurationMs = durationMs
	ttlCtx, ttlCancel := context.WithCancel(context.Background())
	c.cancelTTL = ttlCancel
	go c.ttlLoop(ttlCtx)
}

// --- internal ---

func (c *Controller) tryCleanup() {
	if c.runComplete && c.dispatchIdle {
		c.stopLocked()
	}
}

func (c *Controller) stopLocked() {
	if !c.running {
		c.stopped = true // ensure post-close guard
		return
	}
	c.running = false
	c.stopped = true
	if c.cancelKeep != nil {
		c.cancelKeep()
		c.cancelKeep = nil
	}
	if c.cancelTTL != nil {
		c.cancelTTL()
		c.cancelTTL = nil
	}
	if c.stopFn != nil {
		if err := c.stopFn(); err != nil {
			log.Printf("[typing] stop error: %v", err)
		}
	}
}

func (c *Controller) callStartFn() {
	if c.stopped {
		return
	}
	if c.startFn != nil {
		if err := c.startFn(); err != nil {
			log.Printf("[typing] start error: %v", err)
		}
	}
}

func (c *Controller) keepaliveLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(c.keepaliveMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.stopped || !c.running {
				c.mu.Unlock()
				return
			}
			c.callStartFn()
			c.mu.Unlock()
		}
	}
}

func (c *Controller) ttlLoop(ctx context.Context) {
	timer := time.NewTimer(time.Duration(c.maxDurationMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		c.mu.Lock()
		if c.running {
			log.Printf("[typing] TTL expired after %dms, force-stopping", c.maxDurationMs)
			c.stopLocked()
		}
		c.mu.Unlock()
	}
}
