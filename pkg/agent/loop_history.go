package agent

import (
	"encoding/json"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// SanitizeHistory repairs conversation history for cross-model compatibility.
// It ensures tool call/result pairs are complete and properly ordered,
// regardless of which provider originally created the history.
func SanitizeHistory(msgs []canonical.Message) []canonical.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Pass 1: Collect all tool call IDs and tool result IDs.
	toolCallIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)
	for _, msg := range msgs {
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				toolCallIDs[c.ToolCall.ID] = true
			}
			if c.ToolResult != nil {
				toolResultIDs[c.ToolResult.ToolCallID] = true
			}
		}
	}

	// Pass 2: Filter empty messages and orphaned tool results.
	var filtered []canonical.Message
	for _, msg := range msgs {
		// Skip empty content messages (e.g. Gemini thought-only responses).
		if len(msg.Content) == 0 {
			continue
		}

		// For tool/user messages with only tool_results, check they have matching calls.
		if hasOnlyToolResults(msg) {
			var validResults []canonical.Content
			for _, c := range msg.Content {
				if c.ToolResult != nil && toolCallIDs[c.ToolResult.ToolCallID] {
					validResults = append(validResults, c)
				}
			}
			if len(validResults) == 0 {
				continue // All results are orphaned — drop message.
			}
			msg.Content = validResults
		}

		filtered = append(filtered, msg)
	}

	// Pass 3: Ensure every tool call has a matching result.
	// If not, synthesize a placeholder result immediately after the call.
	var result []canonical.Message
	for _, msg := range filtered {
		result = append(result, msg)

		if msg.Role != "assistant" {
			continue
		}

		// Collect tool call IDs from this assistant message.
		var callIDs []string
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				callIDs = append(callIDs, c.ToolCall.ID)
			}
		}
		if len(callIDs) == 0 {
			continue
		}

		// Check which calls have results somewhere in the remaining history.
		var missingIDs []string
		for _, id := range callIDs {
			if !toolResultIDs[id] {
				missingIDs = append(missingIDs, id)
			}
		}

		// Synthesize placeholder results for missing ones.
		if len(missingIDs) > 0 {
			var placeholders []canonical.Content
			for _, id := range missingIDs {
				placeholders = append(placeholders, canonical.Content{
					Type: "tool_result",
					ToolResult: &canonical.ToolResult{
						ToolCallID: id,
						Content:    "Tool execution was interrupted",
						IsError:    true,
					},
				})
			}
			result = append(result, canonical.Message{
				Role:    "user",
				Content: placeholders,
			})
		}
	}

	return result
}

// hasOnlyToolResults returns true if the message contains only tool_result content.
func hasOnlyToolResults(msg canonical.Message) bool {
	if len(msg.Content) == 0 {
		return false
	}
	for _, c := range msg.Content {
		if c.ToolResult == nil {
			return false
		}
	}
	return true
}

// ExtractToolCalls returns all tool calls from a message.
func ExtractToolCalls(msg canonical.Message) []canonical.ToolCall {
	var calls []canonical.ToolCall
	for _, c := range msg.Content {
		if c.ToolCall != nil {
			calls = append(calls, *c.ToolCall)
		}
	}
	return calls
}

// BuildToolResultMessage creates a tool result message from results.
func BuildToolResultMessage(results []canonical.ToolResult) canonical.Message {
	var content []canonical.Content
	for _, r := range results {
		r := r
		content = append(content, canonical.Content{
			Type:       "tool_result",
			ToolResult: &r,
		})
	}
	return canonical.Message{Role: "user", Content: content}
}

// ExtractText returns the concatenated text content from a message.
func ExtractText(msg canonical.Message) string {
	var text string
	for _, c := range msg.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return text
}

// HasToolCalls returns true if the message contains tool calls.
func HasToolCalls(msg canonical.Message) bool {
	for _, c := range msg.Content {
		if c.ToolCall != nil {
			return true
		}
	}
	return false
}

// placeholderToolResult creates a placeholder for an interrupted tool call.
func placeholderToolResult(toolCallID string) json.RawMessage {
	r := canonical.ToolResult{
		ToolCallID: toolCallID,
		Content:    "Tool execution was interrupted",
		IsError:    true,
	}
	data, _ := json.Marshal(r)
	return data
}
