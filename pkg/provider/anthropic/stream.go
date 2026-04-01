package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

// SSE event types from Anthropic's streaming API.
const (
	eventMessageStart      = "message_start"
	eventContentBlockStart = "content_block_start"
	eventContentBlockDelta = "content_block_delta"
	eventContentBlockStop  = "content_block_stop"
	eventMessageDelta      = "message_delta"
	eventMessageStop       = "message_stop"
	eventPing              = "ping"
	eventError             = "error"
)

type sseEvent struct {
	Event string
	Data  string
}

// toolAccum accumulates tool call fragments for internal accumulation.
type toolAccum struct {
	id       string
	name     string
	inputBuf strings.Builder
}

// ParseSSEStream reads an SSE stream and emits provider.StreamEvents.
// Tool calls are accumulated internally and emitted as complete ToolCall objects.
func ParseSSEStream(r io.Reader, events chan<- provider.StreamEvent) {
	defer close(events)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line for large tool args.
	var current sseEvent
	toolAccums := make(map[int]*toolAccum)
	thinkingChars := 0 // Accumulated thinking text chars for token estimation.

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = dispatch event.
			if current.Event != "" && current.Data != "" {
				processSSEEvent(current, events, toolAccums, &thinkingChars)
			}
			current = sseEvent{}
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			current.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			current.Data = strings.TrimPrefix(line, "data: ")
		}
	}

	// Handle any remaining event.
	if current.Event != "" && current.Data != "" {
		processSSEEvent(current, events, toolAccums, &thinkingChars)
	}
}

func processSSEEvent(evt sseEvent, events chan<- provider.StreamEvent, toolAccums map[int]*toolAccum, thinkingChars *int) {
	switch evt.Event {
	case eventContentBlockDelta:
		var delta struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				Thinking    string `json:"thinking,omitempty"`
				Signature   string `json:"signature,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &delta); err != nil {
			return
		}
		switch delta.Delta.Type {
		case "text_delta":
			events <- provider.StreamEvent{
				Type:  "content_delta",
				Index: delta.Index,
				Delta: &canonical.Content{
					Type: "text",
					Text: delta.Delta.Text,
				},
			}
		case "thinking_delta":
			*thinkingChars += len(delta.Delta.Thinking)
			events <- provider.StreamEvent{
				Type:  "content_delta",
				Index: delta.Index,
				Delta: &canonical.Content{
					Type:     "thinking",
					Thinking: delta.Delta.Thinking,
				},
			}
		case "signature_delta":
			events <- provider.StreamEvent{
				Type:  "content_delta",
				Index: delta.Index,
				Delta: &canonical.Content{
					Type: "thinking",
					Meta: map[string]string{"thinking_signature": delta.Delta.Signature},
				},
			}
		case "input_json_delta":
			// Accumulate tool call input fragments internally.
			if ta, ok := toolAccums[delta.Index]; ok {
				ta.inputBuf.WriteString(delta.Delta.PartialJSON)
			}
		}

	case eventContentBlockStart:
		var block struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type  string          `json:"type"`
				ID    string          `json:"id,omitempty"`
				Name  string          `json:"name,omitempty"`
				Input json.RawMessage `json:"input,omitempty"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &block); err != nil {
			return
		}
		if block.ContentBlock.Type == "tool_use" {
			// Start accumulating this tool call.
			toolAccums[block.Index] = &toolAccum{
				id:   block.ContentBlock.ID,
				name: block.ContentBlock.Name,
			}
		}
		// thinking blocks start — no action needed until deltas arrive.
		if block.ContentBlock.Type == "redacted_thinking" {
			events <- provider.StreamEvent{
				Type: "content_delta",
				Delta: &canonical.Content{
					Type:     "thinking",
					Thinking: "[redacted]",
				},
			}
		}

	case eventContentBlockStop:
		var stop struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &stop); err != nil {
			return
		}
		// If this was a tool_use block, emit the complete tool call.
		if ta, ok := toolAccums[stop.Index]; ok {
			input := ta.inputBuf.String()
			if input == "" {
				input = "{}"
			}
			events <- provider.StreamEvent{
				Type:  "tool_call",
				Index: stop.Index,
				Delta: &canonical.Content{
					ToolCall: &canonical.ToolCall{
						ID:    ta.id,
						Name:  ta.name,
						Input: json.RawMessage(input),
					},
				},
			}
			delete(toolAccums, stop.Index)
		}

	case eventMessageStart:
		// Extract usage from initial message.
		var msg struct {
			Message struct {
				Usage apiUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &msg); err != nil {
			return
		}
		if msg.Message.Usage.InputTokens > 0 {
			events <- provider.StreamEvent{
				Type: "usage",
				Usage: &canonical.Usage{
					InputTokens:   msg.Message.Usage.InputTokens,
					CacheCreation: msg.Message.Usage.CacheCreationInputTokens,
					CacheRead:     msg.Message.Usage.CacheReadInputTokens,
				},
			}
		}

	case eventMessageDelta:
		var delta struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage apiUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &delta); err != nil {
			return
		}
		// Estimate thinking tokens from accumulated thinking content (~4 chars/token).
		// Anthropic doesn't report a separate thinking token count in streaming events.
		estimatedThinking := 0
		if *thinkingChars > 0 {
			estimatedThinking = *thinkingChars / 4
		}
		ev := provider.StreamEvent{
			Type:       "usage",
			StopReason: delta.Delta.StopReason,
			Usage: &canonical.Usage{
				OutputTokens:   delta.Usage.OutputTokens,
				CacheCreation:  delta.Usage.CacheCreationInputTokens,
				CacheRead:      delta.Usage.CacheReadInputTokens,
				ThinkingTokens: estimatedThinking,
				Estimated:      estimatedThinking > 0,
			},
		}
		events <- ev

	case eventMessageStop:
		events <- provider.StreamEvent{Type: "done"}

	case eventError:
		events <- provider.StreamEvent{
			Type:  "error",
			Error: fmt.Errorf("stream error: %s", evt.Data),
		}

	case eventPing:
		// Ignore pings.
	}
}
