package context

import (
	"fmt"
	"sort"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const (
	// DefaultAggregateBudgetChars is the default per-message aggregate
	// character budget for tool results.
	DefaultAggregateBudgetChars = 20000

	// overflowPreviewChars is how many leading characters to keep as a
	// preview when a tool result is persisted to overflow.
	overflowPreviewChars = 500
)

// resultEntry tracks a single tool_result content block for sorting.
type resultEntry struct {
	contentIdx int // index into Message.Content
	chars      int // len(ToolResult.Content)
}

// applyAggregateBudget enforces per-message tool result size limits.
// When the total character count of all tool_result blocks in a message
// exceeds budgetChars, the largest results are persisted to disk via
// the OverflowManager and replaced with a preview + path reference.
//
// Messages are copied before modification — the caller's slice elements
// are not mutated (though the slice itself may be replaced).
func applyAggregateBudget(
	sessionID string,
	history []canonical.Message,
	overflow *OverflowManager,
	budgetChars int,
) []canonical.Message {
	if overflow == nil || budgetChars <= 0 {
		return history
	}

	out := make([]canonical.Message, len(history))
	copy(out, history)

	for i := range out {
		msg := &out[i]
		if !hasToolResults(msg) {
			continue
		}

		// Sum total chars and collect entries.
		var entries []resultEntry
		totalChars := 0
		for j, c := range msg.Content {
			if c.ToolResult != nil {
				chars := len(c.ToolResult.Content)
				entries = append(entries, resultEntry{contentIdx: j, chars: chars})
				totalChars += chars
			}
		}

		if totalChars <= budgetChars {
			continue
		}

		// Sort by size descending — overflow the largest first.
		sort.Slice(entries, func(a, b int) bool {
			return entries[a].chars > entries[b].chars
		})

		// Copy the message before mutating content.
		out[i] = CopyMessageWithAnnotations(out[i])
		msg = &out[i]

		for _, e := range entries {
			if totalChars <= budgetChars {
				break
			}

			c := &msg.Content[e.contentIdx]
			// Deep-copy the ToolResult so we don't mutate the original.
			trCopy := *c.ToolResult
			c.ToolResult = &trCopy
			tr := c.ToolResult
			original := tr.Content

			// Persist to disk.
			path, err := overflow.Persist(sessionID, tr.ToolCallID, original)
			if err != nil {
				// Graceful degradation: keep in memory if disk write fails.
				continue
			}

			// Replace with preview + path.
			preview := original
			if len(preview) > overflowPreviewChars {
				preview = preview[:overflowPreviewChars]
			}
			tr.Content = fmt.Sprintf("%s\n\n[Full result: %s]", preview, path)

			// Update annotation.
			ann := EnsureAnnotations(msg)
			ann.OverflowPath = path
			// Reset token estimate since content changed.
			ann.TokenEstimate = 0

			totalChars -= e.chars
			totalChars += len(tr.Content)
		}
	}

	return out
}

// hasToolResults returns true if the message contains any tool_result blocks.
func hasToolResults(msg *canonical.Message) bool {
	for _, c := range msg.Content {
		if c.ToolResult != nil {
			return true
		}
	}
	return false
}
