package typing

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestController_KeepaliveRefires(t *testing.T) {
	var count atomic.Int32
	c := NewController(
		func() error { count.Add(1); return nil },
		nil,
		50,   // keepalive every 50ms
		5000, // TTL 5s
	)
	c.Start()
	defer c.Stop()

	// Wait for initial + a few keepalives.
	time.Sleep(180 * time.Millisecond)
	got := count.Load()
	if got < 3 {
		t.Errorf("keepalive count = %d, want >= 3 (initial + 2 keepalives)", got)
	}
}

func TestController_TTLAutoStops(t *testing.T) {
	var count atomic.Int32
	c := NewController(
		func() error { count.Add(1); return nil },
		nil,
		20,  // keepalive every 20ms
		100, // TTL 100ms
	)
	c.Start()

	// Wait for TTL to expire.
	time.Sleep(200 * time.Millisecond)

	c.mu.Lock()
	running := c.running
	c.mu.Unlock()

	if running {
		t.Error("controller should be stopped after TTL")
	}

	// No more keepalives after TTL.
	countAtStop := count.Load()
	time.Sleep(100 * time.Millisecond)
	if count.Load() != countAtStop {
		t.Error("keepalive fired after TTL stop")
	}
}

func TestController_DualSignal(t *testing.T) {
	c := NewController(func() error { return nil }, nil, 50, 5000)
	c.Start()

	// MarkRunComplete alone doesn't stop.
	c.MarkRunComplete()
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if !running {
		t.Error("should still be running after only MarkRunComplete")
	}

	// MarkDispatchIdle completes the pair.
	c.MarkDispatchIdle()
	c.mu.Lock()
	running = c.running
	c.mu.Unlock()
	if running {
		t.Error("should be stopped after both signals")
	}
}

func TestController_DualSignal_ReverseOrder(t *testing.T) {
	c := NewController(func() error { return nil }, nil, 50, 5000)
	c.Start()

	c.MarkDispatchIdle()
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if !running {
		t.Error("should still be running after only MarkDispatchIdle")
	}

	c.MarkRunComplete()
	c.mu.Lock()
	running = c.running
	c.mu.Unlock()
	if running {
		t.Error("should be stopped after both signals")
	}
}

func TestController_StopIdempotent(t *testing.T) {
	var stopCount atomic.Int32
	c := NewController(
		func() error { return nil },
		func() error { stopCount.Add(1); return nil },
		50, 5000,
	)
	c.Start()
	c.Stop()
	c.Stop()
	c.Stop()

	if stopCount.Load() != 1 {
		t.Errorf("stopFn called %d times, want 1", stopCount.Load())
	}
}

func TestController_PostCloseStartNotCalled(t *testing.T) {
	var count atomic.Int32
	c := NewController(
		func() error { count.Add(1); return nil },
		nil, 20, 5000,
	)
	c.Start()
	c.Stop()

	countAtStop := count.Load()
	time.Sleep(100 * time.Millisecond)
	if count.Load() != countAtStop {
		t.Errorf("startFn called after Stop: before=%d, after=%d", countAtStop, count.Load())
	}
}

func TestController_StartWhileStopped(t *testing.T) {
	c := NewController(func() error { return nil }, nil, 50, 5000)
	c.Stop() // Stop before Start.
	c.Start() // Should be a no-op.

	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		t.Error("should not start after being stopped")
	}
}

func TestController_StopFnCalled(t *testing.T) {
	var stopped atomic.Bool
	c := NewController(
		func() error { return nil },
		func() error { stopped.Store(true); return nil },
		50, 5000,
	)
	c.Start()
	c.Stop()
	if !stopped.Load() {
		t.Error("stopFn should be called on Stop")
	}
}

func TestController_ExtendTTL(t *testing.T) {
	c := NewController(func() error { return nil }, nil, 20, 80)
	c.Start()

	// Extend TTL before it expires.
	time.Sleep(50 * time.Millisecond)
	c.ExtendTTL(200)

	// Original TTL (80ms) would have expired by now.
	time.Sleep(60 * time.Millisecond)
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if !running {
		t.Error("should still be running after TTL extension")
	}

	// Wait for extended TTL to expire.
	time.Sleep(200 * time.Millisecond)
	c.mu.Lock()
	running = c.running
	c.mu.Unlock()
	if running {
		t.Error("should be stopped after extended TTL")
	}
}
