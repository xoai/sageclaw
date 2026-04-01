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

// oaiToolAccum accumulates tool call fragments for internal accumulation.
type oaiToolAccum struct {
	id       string
	name     string
	argsBuf  strings.Builder
}

// ParseSSEStream reads an OpenAI SSE stream and emits provider.StreamEvents.
// Tool calls are accumulated internally and emitted as complete ToolCall objects.
func ParseSSEStream(r io.Reader, events chan<- provider.StreamEvent) {
	defer close(events)

	toolAccums := make(map[int]*oaiToolAccum)
	var stopReason string
	usageEmitted := false

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line for large tool args.
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Stream terminator — emit accumulated tool calls and done.
		if data == "[DONE]" {
			emitAccumulatedToolCalls(toolAccums, events)
			events <- provider.StreamEvent{Type: "done", StopReason: stopReason}
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (OpenAI sends this at the end with stream_options).
			if chunk.Usage != nil && !usageEmitted {
				u := openAIUsageToCanonical(*chunk.Usage)
				events <- provider.StreamEvent{Type: "usage", Usage: &u}
				usageEmitted = true
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

		// Tool call deltas — accumulate internally.
		for _, tc := range choice.Delta.ToolCalls {
			ta, ok := toolAccums[tc.Index]
			if !ok {
				ta = &oaiToolAccum{}
				toolAccums[tc.Index] = ta
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

		// Finish reason — emit accumulated tool calls when stream signals completion.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			stopReason = mapFinishReason(*choice.FinishReason)
			emitAccumulatedToolCalls(toolAccums, events)
		}

		// Usage bundled with choices (OpenRouter sends usage on the final choice chunk).
		// Dedup: only emit once across both paths.
		if chunk.Usage != nil && !usageEmitted {
			u := openAIUsageToCanonical(*chunk.Usage)
			events <- provider.StreamEvent{Type: "usage", Usage: &u}
			usageEmitted = true
		}
	}
}

// emitAccumulatedToolCalls emits all accumulated tool calls as complete events.
func emitAccumulatedToolCalls(accums map[int]*oaiToolAccum, events chan<- provider.StreamEvent) {
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
	// Clear the map to prevent double-emission.
	for k := range accums {
		delete(accums, k)
	}
}
