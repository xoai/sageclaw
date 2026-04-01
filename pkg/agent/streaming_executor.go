package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

// ConsentCheckFunc checks whether a tool call is approved.
// Returns nil if approved, or a ToolResult with the denial message.
type ConsentCheckFunc func(ctx context.Context, tc canonical.ToolCall) *canonical.ToolResult

// StreamingExecutor manages tool execution with streaming support and
// concurrency. It receives tool calls as they become ready during LLM
// streaming and dispatches them according to their concurrency classification.
type StreamingExecutor struct {
	registry     *tool.Registry
	semaphores   *ResourceSemaphores
	onEvent      EventHandler
	consentCheck ConsentCheckFunc
	speculative  *SpeculativeEngine // Optional: speculative execution.

	// Per-iteration state (reset on StartIteration).
	mu           sync.Mutex
	toolCtx      context.Context        // Context for tool execution (carries iteration info, agent ID, etc.)
	earlyResults map[string]*canonical.ToolResult
	earlyWg      sync.WaitGroup
	started      map[string]bool // Tool call IDs that started early execution.
}

// NewStreamingExecutor creates a new executor.
func NewStreamingExecutor(
	registry *tool.Registry,
	semaphores *ResourceSemaphores,
	onEvent EventHandler,
	consentCheck ConsentCheckFunc,
) *StreamingExecutor {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	return &StreamingExecutor{
		registry:     registry,
		semaphores:   semaphores,
		onEvent:      onEvent,
		consentCheck: consentCheck,
	}
}

// SetSpeculativeEngine adds speculative execution capability.
func (se *StreamingExecutor) SetSpeculativeEngine(spec *SpeculativeEngine) {
	se.speculative = spec
}

// StartIteration prepares the executor for a new iteration.
// Must be called before FeedToolCall or ExecuteRemaining.
// Drains any in-flight goroutines from a previous iteration first.
func (se *StreamingExecutor) StartIteration(ctx context.Context, toolCtx context.Context) {
	// Wait for any in-flight goroutines from the previous iteration
	// to prevent WaitGroup corruption.
	se.earlyWg.Wait()

	se.mu.Lock()
	defer se.mu.Unlock()
	se.toolCtx = toolCtx
	se.earlyResults = make(map[string]*canonical.ToolResult)
	se.started = make(map[string]bool)
}

// FeedToolCall is called when a tool call block is fully accumulated during
// streaming. If the tool is concurrent-safe and auto-approved, execution
// starts immediately in a goroutine. Otherwise, the call is queued for
// post-stream batch execution via ExecuteRemaining.
func (se *StreamingExecutor) FeedToolCall(tc canonical.ToolCall) {
	// Check concurrency safety (registry has its own lock).
	if !se.registry.IsConcurrencySafe(tc.Name) {
		return
	}

	// Copy needed values under the lock, then release before consent check.
	se.mu.Lock()
	toolCtx := se.toolCtx
	consentCheck := se.consentCheck
	se.mu.Unlock()

	// Non-blocking consent check OUTSIDE the mutex: if consent requires
	// user interaction, skip early execution (handled in ExecuteRemaining).
	if consentCheck != nil {
		result := consentCheck(toolCtx, tc)
		if result != nil {
			// Consent denied — store the denial result.
			se.mu.Lock()
			se.earlyResults[tc.ID] = result
			se.started[tc.ID] = true
			se.mu.Unlock()
			return
		}
	}

	se.mu.Lock()
	se.started[tc.ID] = true
	se.earlyWg.Add(1)
	se.mu.Unlock()

	// Fire early execution in goroutine.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[streaming-executor] panic in early execution of %s: %v", tc.Name, r)
				se.mu.Lock()
				se.earlyResults[tc.ID] = &canonical.ToolResult{
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("Tool panicked: %v", r),
					IsError:    true,
				}
				se.mu.Unlock()
			}
			se.earlyWg.Done()
		}()

		se.onEvent(Event{Type: EventToolCallStarted, ToolCall: &tc})

		// Acquire resource semaphore.
		rt := ToolResource(tc.Name)
		if err := se.semaphores.Acquire(toolCtx, rt); err != nil {
			se.mu.Lock()
			se.earlyResults[tc.ID] = &canonical.ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Resource acquisition timeout: %v", err),
				IsError:    true,
			}
			se.mu.Unlock()
			return
		}
		defer se.semaphores.Release(rt)

		result, err := se.registry.Execute(toolCtx, tc.Name, tc.Input)
		if err != nil {
			result = &canonical.ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Tool error: %v", err),
				IsError:    true,
			}
		} else {
			result.ToolCallID = tc.ID
		}

		se.mu.Lock()
		se.earlyResults[tc.ID] = result
		se.mu.Unlock()
	}()
}

// ExecuteRemaining runs all tool calls that weren't started during streaming.
// Must be called after the stream completes.
//
// Contract:
// 1. Wait for all in-flight early executions to complete.
// 2. Partition remaining (non-started) tools into batches and execute.
// 3. Merge early + batch results, return in tool-call order.
func (se *StreamingExecutor) ExecuteRemaining(
	ctx context.Context,
	toolCtx context.Context,
	allCalls []canonical.ToolCall,
	consentCheck ConsentCheckFunc,
) []canonical.ToolResult {
	// Step 1: Wait for all early executions to complete.
	se.earlyWg.Wait()

	// Step 2: Collect remaining (non-started) calls.
	se.mu.Lock()
	var remaining []pendingToolCall
	for _, call := range allCalls {
		if se.started[call.ID] {
			continue // Already executed or has denial result.
		}

		ptc := pendingToolCall{
			call:            call,
			concurrencySafe: se.registry.IsConcurrencySafe(call.Name),
		}

		// Check consent for remaining tools.
		if consentCheck != nil {
			if result := consentCheck(ctx, call); result != nil {
				ptc.consentResult = result
			}
		}

		remaining = append(remaining, ptc)
	}
	se.mu.Unlock()

	// Step 3: Execute remaining in batches.
	batchResults := make(map[string]*canonical.ToolResult)
	if len(remaining) > 0 {
		batches := PartitionBatches(remaining)
		for _, batch := range batches {
			results := se.executeBatch(ctx, toolCtx, batch)
			for id, r := range results {
				batchResults[id] = r
			}
		}
	}

	// Step 4: Merge early + batch results in tool-call order.
	se.mu.Lock()
	defer se.mu.Unlock()

	results := make([]canonical.ToolResult, len(allCalls))
	for i, call := range allCalls {
		if r, ok := se.earlyResults[call.ID]; ok {
			results[i] = *r
		} else if r, ok := batchResults[call.ID]; ok {
			results[i] = *r
		} else {
			results[i] = canonical.ToolResult{
				ToolCallID: call.ID,
				Content:    "Internal error: no result for tool call",
				IsError:    true,
			}
		}
	}
	return results
}

// executeBatch runs a single batch of tool calls.
func (se *StreamingExecutor) executeBatch(
	ctx context.Context,
	toolCtx context.Context,
	batch Batch,
) map[string]*canonical.ToolResult {
	results := make(map[string]*canonical.ToolResult, len(batch.Calls))
	var mu sync.Mutex

	const maxBatchConcurrency = 5

	if batch.Concurrent && len(batch.Calls) > 1 {
		// Concurrent execution — all tools run to completion.
		batchCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		var wg sync.WaitGroup
		sem := make(chan struct{}, maxBatchConcurrency)

		for _, ptc := range batch.Calls {
			ptc := ptc
			wg.Add(1)
			go func() {
				defer wg.Done()

				// Respect concurrency limit.
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-batchCtx.Done():
					mu.Lock()
					results[ptc.call.ID] = &canonical.ToolResult{
						ToolCallID: ptc.call.ID,
						Content:    "Cancelled: batch context expired.",
						IsError:    true,
					}
					mu.Unlock()
					return
				}

				result := se.executeOne(batchCtx, toolCtx, ptc)
				mu.Lock()
				results[ptc.call.ID] = result
				mu.Unlock()
			}()
		}
		wg.Wait()
	} else {
		// Sequential execution.
		for _, ptc := range batch.Calls {
			result := se.executeOne(ctx, toolCtx, ptc)
			results[ptc.call.ID] = result
		}
	}

	// Ensure every call has a result (cancelled tools get synthetic error).
	for _, ptc := range batch.Calls {
		if _, ok := results[ptc.call.ID]; !ok {
			results[ptc.call.ID] = &canonical.ToolResult{
				ToolCallID: ptc.call.ID,
				Content:    "Cancelled: sibling tool failed.",
				IsError:    true,
			}
		}
	}

	return results
}

// executeOne runs a single tool call with resource semaphore.
func (se *StreamingExecutor) executeOne(
	ctx context.Context,
	toolCtx context.Context,
	ptc pendingToolCall,
) *canonical.ToolResult {
	// If consent was denied, return the denial result.
	if ptc.consentResult != nil {
		return ptc.consentResult
	}

	// Check speculative cache before executing.
	if se.speculative != nil {
		if cached := se.speculative.CheckCache(ptc.call); cached != nil {
			se.onEvent(Event{Type: EventToolCallStarted, ToolCall: &ptc.call, Text: "speculative-hit"})
			return cached
		}
	}

	se.onEvent(Event{Type: EventToolCallStarted, ToolCall: &ptc.call})

	// Acquire resource semaphore.
	rt := ToolResource(ptc.call.Name)
	if err := se.semaphores.Acquire(ctx, rt); err != nil {
		return &canonical.ToolResult{
			ToolCallID: ptc.call.ID,
			Content:    fmt.Sprintf("Resource acquisition timeout: %v", err),
			IsError:    true,
		}
	}
	defer se.semaphores.Release(rt)

	result, err := se.registry.Execute(toolCtx, ptc.call.Name, ptc.call.Input)
	if err != nil {
		return &canonical.ToolResult{
			ToolCallID: ptc.call.ID,
			Content:    fmt.Sprintf("Tool error: %v", err),
			IsError:    true,
		}
	}
	result.ToolCallID = ptc.call.ID

	// Fire speculative patterns after successful execution.
	if se.speculative != nil {
		se.speculative.OnToolResult(ptc.call, result, se.registry, func(name string, input json.RawMessage) (*canonical.ToolResult, error) {
			return se.registry.Execute(toolCtx, name, input)
		})
	}

	return result
}

// Abort cancels all in-flight tool executions.
// Called when the stream fails and we need to clean up.
func (se *StreamingExecutor) Abort() {
	// Wait for in-flight goroutines to finish (they'll observe context cancellation).
	se.earlyWg.Wait()
}
