package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

type streamChunk struct {
	ID      string              `json:"id"`
	Choices []streamChunkChoice `json:"choices"`
	Usage   *chatUsage          `json:"usage,omitempty"`
}

type streamChunkChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Role      string         `json:"role,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

// ParseSSEStream reads an OpenAI SSE stream and emits provider.StreamEvents.
func ParseSSEStream(r io.Reader, events chan<- provider.StreamEvent) {
	defer close(events)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Stream terminator.
		if data == "[DONE]" {
			events <- provider.StreamEvent{Type: "done"}
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (OpenAI sends this at the end with stream_options).
			if chunk.Usage != nil {
				events <- provider.StreamEvent{
					Type: "usage",
					Usage: &canonical.Usage{
						InputTokens:  chunk.Usage.PromptTokens,
						OutputTokens: chunk.Usage.CompletionTokens,
					},
				}
			}
			continue
		}

		choice := chunk.Choices[0]

		// Text content delta.
		if choice.Delta.Content != "" {
			events <- provider.StreamEvent{
				Type: "content_delta",
				Delta: &canonical.Content{
					Type: "text",
					Text: choice.Delta.Content,
				},
			}
		}

		// Tool call deltas.
		for _, tc := range choice.Delta.ToolCalls {
			if tc.Function.Name != "" {
				events <- provider.StreamEvent{
					Type: "tool_call",
					Delta: &canonical.Content{
						Type: "tool_call",
						ToolCall: &canonical.ToolCall{
							ID:   tc.ID,
							Name: tc.Function.Name,
						},
					},
				}
			}
		}

		// Finish reason.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			// Usage comes in a separate chunk, just note completion.
		}
	}
}
