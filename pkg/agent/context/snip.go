package context

import (
	"context"
	"fmt"
	"log"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// readOnlyTools is the source of truth for which tools produce read-only
// results that can be safely snipped after aging out.
var readOnlyTools = map[string]bool{
	"glob":           true,
	"grep":           true,
	"read_file":      true,
	"list_files":     true,
	"list_tasks":     true,
	"memory_search":  true,
	"memory_list":    true,
	"web_fetch":      true,
	"web_search":     true,
	"datetime":       true,
	"list_agents":    true,
}

// applySnip replaces aged read-only tool results with compact markers.
// Messages are copied before modification — the caller's slice elements
// are not mutated.
//
// Rules:
//   - Only snips messages where all tool results are read-only (snippable)
//   - Only snips when currentIteration - msg.Iteration >= snipAge
//   - Never snips the last protectedCount tool_result messages
//   - Sets Annotations.Snipped = true, resets TokenEstimate
func applySnip(
	ctx context.Context,
	history []canonical.Message,
	currentIteration int,
	snipAge int,
	protectedCount int,
	llmCaller LLMCaller,
) []canonical.Message {
	if snipAge <= 0 {
		return history
	}
	if protectedCount < 0 {
		protectedCount = 3
	}

	// Build toolNameByCallID map from assistant messages' tool calls.
	toolNameByCallID := buildToolNameMap(history)

	// Find indices of the last N tool_result messages (protected zone).
	protected := protectedToolResultIndices(history, protectedCount)

	out := make([]canonical.Message, len(history))
	copy(out, history)

	for i := range out {
		if protected[i] {
			continue
		}

		ann := out[i].Annotations
		if ann == nil {
			continue
		}
		if ann.Snipped {
			continue // Already snipped.
		}

		// Check age.
		age := currentIteration - ann.Iteration
		if age < snipAge {
			continue
		}

		// Check snippability.
		if !IsSnippable(out[i], toolNameByCallID, readOnlyTools) {
			continue
		}

		// Copy and replace content.
		out[i] = CopyMessageWithAnnotations(out[i])
		for j := range out[i].Content {
			if out[i].Content[j].ToolResult == nil {
				continue
			}
			tr := out[i].Content[j].ToolResult
			toolName := toolNameByCallID[tr.ToolCallID]
			originalSize := len(tr.Content)

			// Lazy summary: generate on-demand only for results about to be snipped.
			if ann.Summary == "" && llmCaller != nil && originalSize > 100 {
				input := "Tool: " + toolName + "\nResult: "
				content := tr.Content
				if len(content) > 2000 {
					content = content[:2000] + "... [truncated]"
				}
				input += content
				if summary, err := llmCaller(ctx, toolSummaryPrompt, input, toolSummaryTimeout); err == nil && summary != "" {
					if len(summary) > 200 {
						summary = summary[:197] + "..."
					}
					ann.Summary = summary
				} else if err != nil {
					log.Printf("[context-pipeline] lazy snip summary failed for %s: %v", toolName, err)
				}
			}

			// Use summary if available, otherwise static replacement.
			replacement := ""
			if ann.Summary != "" {
				replacement = fmt.Sprintf("[Snipped: %s — %s (%d chars)]", toolName, ann.Summary, originalSize)
			} else {
				replacement = fmt.Sprintf("[Snipped: %s result from iteration %d — %d chars]", toolName, ann.Iteration, originalSize)
			}

			// Deep-copy ToolResult before mutating.
			trCopy := *tr
			trCopy.Content = replacement
			out[i].Content[j].ToolResult = &trCopy
		}

		// Update annotations.
		out[i].Annotations.Snipped = true
		out[i].Annotations.TokenEstimate = 0
	}

	return out
}

// buildToolNameMap builds a map from ToolCallID to tool name by scanning
// assistant messages for tool_call content blocks.
func buildToolNameMap(msgs []canonical.Message) map[string]string {
	m := make(map[string]string)
	for _, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				m[c.ToolCall.ID] = c.ToolCall.Name
			}
		}
	}
	return m
}

// protectedToolResultIndices returns a set of message indices for the last
// N messages that contain tool_result content blocks.
func protectedToolResultIndices(msgs []canonical.Message, n int) map[int]bool {
	result := make(map[int]bool)
	count := 0
	for i := len(msgs) - 1; i >= 0 && count < n; i-- {
		for _, c := range msgs[i].Content {
			if c.ToolResult != nil {
				result[i] = true
				count++
				break
			}
		}
	}
	return result
}
