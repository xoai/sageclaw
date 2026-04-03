package agent

import (
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tokenizer"
)

// NeedsCompaction checks if the message history needs compaction.
// Primary trigger: token budget (context window usage exceeds tokenRatio).
// Fallback: message count threshold as a safety net.
// The token-budget approach handles cases where a few messages with large
// tool results consume most of the context window.
func NeedsCompaction(msgs []canonical.Message, contextWindow, messageThreshold int, tokenRatio float64) bool {
	if messageThreshold <= 0 {
		messageThreshold = 50
	}
	if tokenRatio <= 0 {
		tokenRatio = 0.60 // Trigger at 60% to leave headroom for response + tools.
	}

	// Skip if too few messages — not worth compacting.
	if len(msgs) <= 6 {
		return false
	}

	// Token-budget check (primary trigger).
	// Estimate context size and compare against model's context window.
	if contextWindow > 0 {
		counter, err := tokenizer.Get()
		if err == nil {
			total := counter.CountMessages(msgs)
			if float64(total) > float64(contextWindow)*tokenRatio {
				return true
			}
		}
	}

	// Message-count fallback — catches edge cases where token counting
	// is unavailable or context window is unknown.
	return len(msgs) > messageThreshold
}

// CompactionSplit splits messages into "to compact" and "to keep" portions.
// Keeps the last keepRatio (default 30%) of messages, minimum minKeep (default 4).
func CompactionSplit(msgs []canonical.Message, keepRatio float64, minKeep int) (toCompact, toKeep []canonical.Message) {
	if keepRatio <= 0 {
		keepRatio = 0.30
	}
	if minKeep <= 0 {
		minKeep = 4
	}

	keepCount := int(float64(len(msgs)) * keepRatio)
	if keepCount < minKeep {
		keepCount = minKeep
	}
	if keepCount >= len(msgs) {
		return nil, msgs // Nothing to compact.
	}

	splitAt := len(msgs) - keepCount

	// Adjust split to not break tool_use/tool_result pairs.
	// Move forward past orphaned tool_results at the split boundary.
	for splitAt < len(msgs)-1 {
		if !hasToolResultContent(msgs[splitAt]) {
			break
		}
		splitAt++ // Include orphaned tool_result in toCompact.
	}

	// Also move backward if split lands right after an assistant with tool_calls
	// whose results are in toKeep. We want the tool_call to stay with its results.
	if splitAt > 0 && splitAt < len(msgs) {
		prev := msgs[splitAt-1]
		if prev.Role == "assistant" && HasToolCalls(prev) {
			// The assistant's tool results should be in the next message(s).
			// Move split back to include the assistant message in toKeep.
			splitAt--
		}
	}

	return msgs[:splitAt], msgs[splitAt:]
}

// hasToolResultContent returns true if the message contains any tool_result content.
func hasToolResultContent(msg canonical.Message) bool {
	for _, c := range msg.Content {
		if c.ToolResult != nil {
			return true
		}
	}
	return false
}

// InjectSummary creates a summary message to replace compacted messages.
func InjectSummary(summary string, toKeep []canonical.Message) []canonical.Message {
	summaryMsg := canonical.Message{
		Role: "assistant",
		Content: []canonical.Content{{
			Type: "text",
			Text: "[Previous conversation summary]\n\n" + strings.TrimSpace(summary),
		}},
	}

	result := make([]canonical.Message, 0, 1+len(toKeep))
	result = append(result, summaryMsg)
	result = append(result, toKeep...)
	return result
}
