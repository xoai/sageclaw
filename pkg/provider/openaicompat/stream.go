package openaicompat

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

// quirksStreamChunk extends the OpenAI stream chunk with provider-specific fields.
type quirksStreamChunk struct {
	ID      string                   `json:"id"`
	Choices []quirksStreamChoice     `json:"choices"`
	Usage   *quirksUsage             `json:"usage,omitempty"`
}

type quirksStreamChoice struct {
	Delta        quirksStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type quirksStreamDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []quirksToolCall `json:"tool_calls,omitempty"`
}

type quirksToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Index    int            `json:"index"`
	Function quirksFunction `json:"function"`
}

type quirksFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type quirksUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	PromptTokensDetails     *quirksPromptDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *quirksCompletionDetails `json:"completion_tokens_details,omitempty"`
}

type quirksPromptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type quirksCompletionDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// toolAccum accumulates incremental tool call fragments.
type toolAccum struct {
	id      string
	name    string
	argsBuf strings.Builder
}

// parseQuirksStream reads an OpenAI-compatible SSE stream with quirks support.
// It handles provider-specific fields like DeepSeek's reasoning_content.
func parseQuirksStream(r io.Reader, events chan<- provider.StreamEvent, quirks Quirks) {
	defer close(events)

	accums := make(map[int]*toolAccum)
	var stopReason string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			emitToolCalls(accums, events)
			events <- provider.StreamEvent{Type: "done", StopReason: stopReason}
			return
		}

		var chunk quirksStreamChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}

		// Usage-only chunk.
		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				u := quirksUsageToCanonical(chunk.Usage)
				events <- provider.StreamEvent{Type: "usage", Usage: &u}
			}
			continue
		}

		choice := chunk.Choices[0]

		// Text content.
		if choice.Delta.Content != "" {
			events <- provider.StreamEvent{
				Type: "content_delta",
				Delta: &canonical.Content{Type: "text", Text: choice.Delta.Content},
			}
		}

		// Thinking/reasoning content (dynamic quirks field).
		if quirks.ThinkingField != "" {
			if thinking := extractDeltaField([]byte(data), quirks.ThinkingField); thinking != "" {
				events <- provider.StreamEvent{
					Type: "content_delta",
					Delta: &canonical.Content{Type: "thinking", Thinking: thinking},
				}
			}
		}

		// Tool calls — accumulate internally.
		for _, tc := range choice.Delta.ToolCalls {
			ta, ok := accums[tc.Index]
			if !ok {
				ta = &toolAccum{}
				accums[tc.Index] = ta
			}
			if tc.ID != "" {
				ta.id = tc.ID
			}
			if tc.Function.Name != "" {
				ta.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				ta.argsBuf.WriteString(tc.Function.Arguments)
			}
		}

		// Finish reason.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			stopReason = mapFinishReason(*choice.FinishReason)
			emitToolCalls(accums, events)
		}
	}
}

func emitToolCalls(accums map[int]*toolAccum, events chan<- provider.StreamEvent) {
	for idx, ta := range accums {
		input := ta.argsBuf.String()
		if input == "" {
			input = "{}"
		}
		events <- provider.StreamEvent{
			Type:  "tool_call",
			Index: idx,
			Delta: &canonical.Content{
				ToolCall: &canonical.ToolCall{
					ID:    ta.id,
					Name:  ta.name,
					Input: json.RawMessage(input),
				},
			},
		}
	}
	for k := range accums {
		delete(accums, k)
	}
}

// extractDeltaField extracts a named string field from the first choice's delta
// using dynamic map-based parsing. Supports any provider-specific thinking field.
func extractDeltaField(data []byte, field string) string {
	var raw struct {
		Choices []struct {
			Delta map[string]any `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &raw) != nil || len(raw.Choices) == 0 {
		return ""
	}
	val, _ := raw.Choices[0].Delta[field].(string)
	return val
}

// quirksUsageToCanonical converts compat usage (with optional detail breakdowns).
func quirksUsageToCanonical(u *quirksUsage) canonical.Usage {
	usage := canonical.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil {
		usage.CacheRead = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		usage.ThinkingTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return usage
}

func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}
