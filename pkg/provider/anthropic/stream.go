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
	eventMessageStart     = "message_start"
	eventContentBlockStart = "content_block_start"
	eventContentBlockDelta = "content_block_delta"
	eventContentBlockStop  = "content_block_stop"
	eventMessageDelta     = "message_delta"
	eventMessageStop      = "message_stop"
	eventPing             = "ping"
	eventError            = "error"
)

type sseEvent struct {
	Event string
	Data  string
}

// ParseSSEStream reads an SSE stream and emits provider.StreamEvents.
func ParseSSEStream(r io.Reader, events chan<- provider.StreamEvent) {
	defer close(events)

	scanner := bufio.NewScanner(r)
	var current sseEvent

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = dispatch event.
			if current.Event != "" && current.Data != "" {
				processSSEEvent(current, events)
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
		processSSEEvent(current, events)
	}
}

func processSSEEvent(evt sseEvent, events chan<- provider.StreamEvent) {
	switch evt.Event {
	case eventContentBlockDelta:
		var delta struct {
			Index int `json:"index"`
			Delta struct {
				Type          string `json:"type"`
				Text          string `json:"text,omitempty"`
				PartialJSON   string `json:"partial_json,omitempty"`
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
		case "input_json_delta":
			// Tool call input arrives as partial JSON fragments.
			events <- provider.StreamEvent{
				Type:  "tool_call",
				Index: delta.Index,
				Delta: &canonical.Content{
					Type:      "tool_call",
					ToolInput: delta.Delta.PartialJSON,
				},
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
			events <- provider.StreamEvent{
				Type:  "tool_call",
				Index: block.Index,
				Delta: &canonical.Content{
					Type:       "tool_call",
					ToolCallID: block.ContentBlock.ID,
					ToolName:   block.ContentBlock.Name,
				},
			}
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
		ev := provider.StreamEvent{
			Type:       "usage",
			StopReason: delta.Delta.StopReason,
			Usage: &canonical.Usage{
				OutputTokens: delta.Usage.OutputTokens,
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
