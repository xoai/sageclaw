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
			Delta struct {
				Type  string `json:"type"`
				Text  string `json:"text,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &delta); err != nil {
			return
		}
		if delta.Delta.Type == "text_delta" {
			events <- provider.StreamEvent{
				Type: "content_delta",
				Delta: &canonical.Content{
					Type: "text",
					Text: delta.Delta.Text,
				},
			}
		}

	case eventContentBlockStart:
		var block struct {
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
				Type: "tool_call",
				Delta: &canonical.Content{
					Type: "tool_call",
					ToolCall: &canonical.ToolCall{
						ID:   block.ContentBlock.ID,
						Name: block.ContentBlock.Name,
					},
				},
			}
		}

	case eventMessageDelta:
		var delta struct {
			Usage apiUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &delta); err != nil {
			return
		}
		events <- provider.StreamEvent{
			Type: "usage",
			Usage: &canonical.Usage{
				InputTokens:  delta.Usage.InputTokens,
				OutputTokens: delta.Usage.OutputTokens,
			},
		}

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
