package agent

import "github.com/xoai/sageclaw/pkg/canonical"

// pendingToolCall wraps a tool call with execution metadata.
type pendingToolCall struct {
	call            canonical.ToolCall
	concurrencySafe bool
	consentResult   *canonical.ToolResult // nil = approved, non-nil = consent denied.
}

// Batch groups tool calls for execution.
type Batch struct {
	Concurrent bool
	Calls      []pendingToolCall
}

// PartitionBatches groups tool calls into concurrent and exclusive batches,
// preserving order.
//
// Input:  [read_file, grep, edit_file, read_file, glob]
// Output: [
//
//	{Concurrent: true,  Calls: [read_file, grep]},
//	{Concurrent: false, Calls: [edit_file]},
//	{Concurrent: true,  Calls: [read_file, glob]},
//
// ]
func PartitionBatches(calls []pendingToolCall) []Batch {
	if len(calls) == 0 {
		return nil
	}

	var batches []Batch
	current := Batch{
		Concurrent: calls[0].concurrencySafe,
		Calls:      []pendingToolCall{calls[0]},
	}

	for i := 1; i < len(calls); i++ {
		if calls[i].concurrencySafe == current.Concurrent {
			current.Calls = append(current.Calls, calls[i])
		} else {
			batches = append(batches, current)
			current = Batch{
				Concurrent: calls[i].concurrencySafe,
				Calls:      []pendingToolCall{calls[i]},
			}
		}
	}
	batches = append(batches, current)
	return batches
}
