package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResourceSemaphore_AcquireRelease(t *testing.T) {
	rs := NewResourceSemaphores()
	ctx := context.Background()

	// Acquire and release filesystem (limit 3).
	for i := 0; i < 3; i++ {
		if err := rs.Acquire(ctx, ResourceFilesystem); err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
	}
	// All 3 slots taken — next acquire should timeout quickly.
	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := rs.Acquire(shortCtx, ResourceFilesystem); err == nil {
		t.Error("expected acquire to fail when all slots taken")
	}
	// Release one and try again.
	rs.Release(ResourceFilesystem)
	if err := rs.Acquire(ctx, ResourceFilesystem); err != nil {
		t.Fatal("acquire after release should succeed")
	}
	// Clean up.
	for i := 0; i < 3; i++ {
		rs.Release(ResourceFilesystem)
	}
}

func TestResourceSemaphore_ContextCancellation(t *testing.T) {
	rs := NewResourceSemaphores()
	// Fill the shell slot.
	if err := rs.Acquire(context.Background(), ResourceShell); err != nil {
		t.Fatal(err)
	}
	// Try to acquire with a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rs.Acquire(ctx, ResourceShell); err == nil {
		t.Error("expected error on cancelled context")
	}
	rs.Release(ResourceShell)
}

func TestResourceSemaphore_ConcurrentAcquire(t *testing.T) {
	rs := NewResourceSemaphores()
	ctx := context.Background()

	// Verify that network semaphore allows exactly 5 concurrent acquires.
	var maxConcurrent int64
	var current int64
	var wg sync.WaitGroup
	const workers = 10

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rs.Acquire(ctx, ResourceNetwork); err != nil {
				return
			}
			defer rs.Release(ResourceNetwork)

			cur := atomic.AddInt64(&current, 1)
			defer atomic.AddInt64(&current, -1)

			// Record max concurrency.
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if cur <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond) // Hold briefly.
		}()
	}
	wg.Wait()

	if maxConcurrent > 5 {
		t.Errorf("max concurrent network acquires = %d, want <= 5", maxConcurrent)
	}
}

func TestResourceSemaphore_ResourceNone(t *testing.T) {
	rs := NewResourceSemaphores()
	// ResourceNone should always succeed without blocking.
	for i := 0; i < 100; i++ {
		if err := rs.Acquire(context.Background(), ResourceNone); err != nil {
			t.Fatalf("ResourceNone acquire failed: %v", err)
		}
		rs.Release(ResourceNone) // Should be a no-op.
	}
}

func TestToolResource(t *testing.T) {
	tests := []struct {
		name     string
		expected ResourceType
	}{
		{"read_file", ResourceFilesystem},
		{"write_file", ResourceFilesystem},
		{"edit", ResourceFilesystem},
		{"list_directory", ResourceFilesystem},
		{"web_fetch", ResourceNetwork},
		{"web_search", ResourceNetwork},
		{"browser_navigate", ResourceNetwork},
		{"execute_command", ResourceShell},
		{"memory_search", ResourceNone},
		{"datetime", ResourceNone},
		{"unknown_tool", ResourceNone},
	}
	for _, tt := range tests {
		got := ToolResource(tt.name)
		if got != tt.expected {
			t.Errorf("ToolResource(%q) = %d, want %d", tt.name, got, tt.expected)
		}
	}
}
