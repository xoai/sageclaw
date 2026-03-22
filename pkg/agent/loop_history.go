package agent

import (
	"encoding/json"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// SanitizeHistory repairs 4 anomaly types in conversation history.
// This prevents LLM API errors from malformed history.
func SanitizeHistory(msgs []canonical.Message) []canonical.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Collect all tool call IDs from assistant messages.
	toolCallIDs := make(map[string]bool)
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			for _, c := range msg.Content {
				if c.ToolCall != nil {
					toolCallIDs[c.ToolCall.ID] = true
				}
			}
		}
	}

	var result []canonical.Message
	for i, msg := range msgs {
		switch msg.Role {
		case "tool":
			// Anomaly 1: Orphaned tool messages with no preceding assistant tool_call.
			if i == 0 || msgs[i-1].Role != "assistant" {
				continue // Remove orphaned tool message.
			}
			// Anomaly 2: Tool results with no matching call ID.
			hasMatch := false
			for _, c := range msg.Content {
				if c.ToolResult != nil && toolCallIDs[c.ToolResult.ToolCallID] {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				continue // Remove unmatched tool result.
			}
			result = append(result, msg)

		case "assistant":
			// Anomaly 3: Assistant tool_calls with no results.
			// Check if the NEXT message has matching results.
			hasToolCalls := false
			for _, c := range msg.Content {
				if c.ToolCall != nil {
					hasToolCalls = true
					break
				}
			}

			if hasToolCalls {
				// Check if results exist in subsequent messages.
				allResultsPresent := true
				for _, c := range msg.Content {
					if c.ToolCall == nil {
						continue
					}
					found := false
					for j := i + 1; j < len(msgs); j++ {
						for _, rc := range msgs[j].Content {
							if rc.ToolResult != nil && rc.ToolResult.ToolCallID == c.ToolCall.ID {
								found = true
								break
							}
						}
						if found {
							break
						}
					}
					if !found {
						allResultsPresent = false
						// Synthesize placeholder result.
						placeholder := canonical.Message{
							Role: "user",
							Content: []canonical.Content{{
								Type: "tool_result",
								ToolResult: &canonical.ToolResult{
									ToolCallID: c.ToolCall.ID,
									Content:    "Tool execution was interrupted",
									IsError:    true,
								},
							}},
						}
						// Insert after this message.
						result = append(result, msg)
						result = append(result, placeholder)
					}
				}
				if allResultsPresent {
					result = append(result, msg)
				}
			} else {
				result = append(result, msg)
			}

		default:
			result = append(result, msg)
		}
	}

	return result
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
