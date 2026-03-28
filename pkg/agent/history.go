package agent

import (
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tokenizer"
)

// HistoryConfig controls the history pipeline behavior.
type HistoryConfig struct {
	ContextWindow int // Max tokens for the model (e.g., 200000 for Claude).
	MaxRatio      float64 // Target ratio of context to use (default 0.85).
}

// DefaultHistoryConfig returns sensible defaults for Claude Sonnet.
func DefaultHistoryConfig() HistoryConfig {
	return HistoryConfig{
		ContextWindow: 200000,
		MaxRatio:      0.85,
	}
}

// PrepareHistory runs the 3-stage history pipeline:
// 1. LimitTurns — drop oldest turns to fit context window
// 2. PruneToolResults — trim large tool results
// 3. Sanitize — repair orphaned tool_use/tool_result pairs
//
// Returns the processed messages ready for the LLM.
func PrepareHistory(msgs []canonical.Message, systemTokens int, cfg HistoryConfig) []canonical.Message {
	if len(msgs) == 0 {
		return msgs
	}
	if cfg.ContextWindow == 0 {
		cfg = DefaultHistoryConfig()
	}
	if cfg.MaxRatio == 0 {
		cfg.MaxRatio = 0.85
	}

	budget := int(float64(cfg.ContextWindow) * cfg.MaxRatio) - systemTokens
	if budget < 1000 {
		budget = 1000
	}

	result := limitTurns(msgs, budget)
	result = pruneToolResults(result, cfg.ContextWindow)
	result = sanitize(result)
	return result
}

// limitTurns keeps the last N user turns that fit within the token budget.
// Protected: first user message is always kept.
func limitTurns(msgs []canonical.Message, budget int) []canonical.Message {
	counter, err := tokenizer.Get()
	if err != nil {
		return msgs // Can't count — return as-is.
	}

	total := counter.CountMessages(msgs)
	if total <= budget {
		return msgs // Fits, no trimming needed.
	}

	// Identify turn boundaries (each user message starts a new turn).
	type turn struct {
		startIdx int
		endIdx   int // Inclusive — includes assistant + tool messages after the user msg.
		tokens   int
	}

	var turns []turn
	currentStart := -1
	for i, msg := range msgs {
		if msg.Role == "user" {
			if currentStart >= 0 {
				// Close previous turn.
				t := turn{startIdx: currentStart, endIdx: i - 1}
				t.tokens = counter.CountMessages(msgs[t.startIdx : t.endIdx+1])
				turns = append(turns, t)
			}
			currentStart = i
		}
	}
	// Close last turn.
	if currentStart >= 0 {
		t := turn{startIdx: currentStart, endIdx: len(msgs) - 1}
		t.tokens = counter.CountMessages(msgs[t.startIdx : t.endIdx+1])
		turns = append(turns, t)
	}

	if len(turns) <= 1 {
		return msgs // Only one turn, can't trim further.
	}

	// Keep the first turn (protected) + as many recent turns as fit.
	firstTurn := turns[0]
	remaining := budget - firstTurn.tokens
	if remaining <= 0 {
		// Even the first turn exceeds budget — return just the last turn.
		lastTurn := turns[len(turns)-1]
		return msgs[lastTurn.startIdx : lastTurn.endIdx+1]
	}

	// Walk backwards from most recent turn, accumulating tokens.
	var keepTurns []turn
	for i := len(turns) - 1; i >= 1; i-- {
		if remaining-turns[i].tokens < 0 {
			break
		}
		remaining -= turns[i].tokens
		keepTurns = append([]turn{turns[i]}, keepTurns...)
	}

	// Build result: first turn + kept turns.
	var result []canonical.Message
	result = append(result, msgs[firstTurn.startIdx:firstTurn.endIdx+1]...)
	for _, t := range keepTurns {
		result = append(result, msgs[t.startIdx:t.endIdx+1]...)
	}
	return result
}

// pruneToolResults trims large tool results to save context space.
func pruneToolResults(msgs []canonical.Message, contextWindow int) []canonical.Message {
	counter, err := tokenizer.Get()
	if err != nil {
		return msgs
	}

	total := counter.CountMessages(msgs)
	ratio := float64(total) / float64(contextWindow)

	if ratio < 0.3 {
		return msgs // Under threshold, no pruning.
	}

	// Find which messages are "protected" (last 3 assistant messages).
	protectedSet := make(map[int]bool)
	assistantCount := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			protectedSet[i] = true
			assistantCount++
			if assistantCount >= 3 {
				break
			}
		}
	}

	result := make([]canonical.Message, len(msgs))
	copy(result, msgs)

	for i := range result {
		if protectedSet[i] {
			continue
		}

		for j, content := range result[i].Content {
			if content.ToolResult == nil {
				continue
			}

			text := content.ToolResult.Content
			if len(text) < 4000 {
				continue
			}

			if ratio >= 0.5 {
				// Hard clear.
				result[i].Content[j].ToolResult = &canonical.ToolResult{
					ToolCallID: content.ToolResult.ToolCallID,
					Content:    fmt.Sprintf("[Tool result cleared — %d chars removed]", len(text)),
				}
			} else {
				// Soft trim: keep first 1500 + last 1500.
				trimmed := text[:1500] + "\n\n[... " + fmt.Sprintf("%d", len(text)-3000) + " chars trimmed ...]\n\n" + text[len(text)-1500:]
				result[i].Content[j].ToolResult = &canonical.ToolResult{
					ToolCallID: content.ToolResult.ToolCallID,
					Content:    trimmed,
				}
			}
		}
	}
	return result
}

// sanitize repairs orphaned tool_use/tool_result pairs.
func sanitize(msgs []canonical.Message) []canonical.Message {
	// Build a set of all tool_use IDs and tool_result IDs.
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)

	for _, msg := range msgs {
		for _, content := range msg.Content {
			if content.ToolCall != nil {
				toolUseIDs[content.ToolCall.ID] = true
			}
			if content.ToolResult != nil {
				toolResultIDs[content.ToolResult.ToolCallID] = true
			}
		}
	}

	var result []canonical.Message
	for _, msg := range msgs {
		var filteredContent []canonical.Content
		for _, content := range msg.Content {
			// Remove orphaned tool_results (no matching tool_use).
			if content.ToolResult != nil {
				if !toolUseIDs[content.ToolResult.ToolCallID] {
					continue // Orphaned — skip.
				}
			}
			filteredContent = append(filteredContent, content)
		}

		if len(filteredContent) == 0 {
			// All content was filtered out — skip the entire message.
			continue
		}

		newMsg := canonical.Message{Role: msg.Role, Content: filteredContent}
		result = append(result, newMsg)
	}

	// Add synthetic results for orphaned tool_use (no matching tool_result).
	for i, msg := range result {
		for _, content := range msg.Content {
			if content.ToolCall != nil && !toolResultIDs[content.ToolCall.ID] {
				// Add synthetic tool result after this message.
				syntheticResult := canonical.Message{
					Role: "user",
					Content: []canonical.Content{{
						Type: "tool_result",
						ToolResult: &canonical.ToolResult{
							ToolCallID: content.ToolCall.ID,
							Content:    "[Result unavailable — message was pruned]",
						},
					}},
				}
				// Insert after the current message.
				newResult := make([]canonical.Message, 0, len(result)+1)
				newResult = append(newResult, result[:i+1]...)
				newResult = append(newResult, syntheticResult)
				newResult = append(newResult, result[i+1:]...)
				result = newResult
				toolResultIDs[content.ToolCall.ID] = true // Mark as resolved.
			}
		}
	}

	return result
}

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
