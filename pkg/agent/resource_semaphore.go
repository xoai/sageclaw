package agent

import (
	"context"
	"strings"
	"time"
)

// ResourceType classifies a tool's primary resource usage.
// Ordered for acquisition ordering (deadlock prevention):
// always acquire in ascending ResourceType order.
type ResourceType int

const (
	ResourceShell      ResourceType = 0  // Acquire first.
	ResourceFilesystem ResourceType = 1
	ResourceNetwork    ResourceType = 2
	ResourceNone       ResourceType = 99 // In-memory only — no semaphore needed.
)

const acquireTimeout = 10 * time.Second

// ResourceSemaphores provides process-wide concurrency limits for shared
// resources. Used by the streaming executor and multi-agent team execution.
type ResourceSemaphores struct {
	filesystem chan struct{} // max 3 concurrent file ops
	network    chan struct{} // max 5 concurrent HTTP requests
	shell      chan struct{} // max 1 concurrent shell execution
}

// NewResourceSemaphores creates semaphores with default limits.
func NewResourceSemaphores() *ResourceSemaphores {
	return &ResourceSemaphores{
		filesystem: make(chan struct{}, 3),
		network:    make(chan struct{}, 5),
		shell:      make(chan struct{}, 1),
	}
}

// Acquire blocks until the resource is available, ctx is cancelled,
// or the per-acquire timeout (10s) expires.
func (rs *ResourceSemaphores) Acquire(ctx context.Context, rt ResourceType) error {
	ch := rs.channel(rt)
	if ch == nil {
		return nil // ResourceNone — no semaphore needed.
	}

	// Per-acquire timeout independent of parent context (deadlock prevention).
	timeoutCtx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()

	select {
	case ch <- struct{}{}:
		return nil
	case <-timeoutCtx.Done():
		return timeoutCtx.Err()
	}
}

// Release frees the resource. Must be called after Acquire succeeds.
func (rs *ResourceSemaphores) Release(rt ResourceType) {
	ch := rs.channel(rt)
	if ch == nil {
		return
	}
	<-ch
}

func (rs *ResourceSemaphores) channel(rt ResourceType) chan struct{} {
	switch rt {
	case ResourceFilesystem:
		return rs.filesystem
	case ResourceNetwork:
		return rs.network
	case ResourceShell:
		return rs.shell
	default:
		return nil
	}
}

// ToolResource returns the resource type for a tool name.
func ToolResource(name string) ResourceType {
	switch {
	case name == "read_file" || name == "write_file" || name == "edit" ||
		name == "list_directory" || name == "create_file":
		return ResourceFilesystem
	case name == "web_fetch" || name == "web_search" ||
		strings.HasPrefix(name, "browser"):
		return ResourceNetwork
	case name == "execute_command":
		return ResourceShell
	default:
		return ResourceNone
	}
}
