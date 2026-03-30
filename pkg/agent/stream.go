package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

// streamResult holds the accumulated result of consuming a ChatStream.
type streamResult struct {
	Message    canonical.Message // Fully assembled assistant message.
	Usage      canonical.Usage
	StopReason string
	Error      error
}

// consumeStream reads all events from a ChatStream channel and accumulates
// them into a complete message. Emits EventChunk for each text delta.
//
// Tool call deltas are accumulated by index — partial JSON fragments are
// concatenated until the stream completes, then parsed as complete JSON.
func consumeStream(
	ctx context.Context,
	stream <-chan provider.StreamEvent,
	sessionID string,
	iteration int,
	onEvent EventHandler,
) streamResult {
	var textBuf strings.Builder
	var thinkingBuf strings.Builder
	var thinkingSig string
	toolCallBuilders := map[int]*toolCallBuilder{}
	var completeToolCalls []canonical.Content // From providers that accumulate internally.
	var usage canonical.Usage
	var stopReason string
	var streamErr error

	for ev := range stream {
		switch ev.Type {
		case "content_delta":
			if ev.Delta == nil {
				continue
			}
			if ev.Delta.Type == "thinking" {
				// Accumulate thinking text and signature.
				if ev.Delta.Thinking != "" {
					thinkingBuf.WriteString(ev.Delta.Thinking)
				}
				if sig, ok := ev.Delta.Meta["thinking_signature"]; ok && sig != "" {
					thinkingSig = sig
				}
				continue
			}
			if ev.Delta.Text != "" {
				textBuf.WriteString(ev.Delta.Text)
				// Emit real-time delta to subscribers.
				onEvent(Event{
					Type:      EventChunk,
					SessionID: sessionID,
					Text:      ev.Delta.Text,
					Iteration: iteration,
				})
			}

		case "tool_call":
			if ev.Delta == nil {
				continue
			}
			// Complete path: provider already accumulated the tool call.
			if ev.Delta.ToolCall != nil {
				completeToolCalls = append(completeToolCalls, canonical.Content{
					Type:     "tool_call",
					ToolCall: ev.Delta.ToolCall,
				})
				continue
			}
			// Delta path: accumulate partial fragments.
			idx := ev.Index
			b, ok := toolCallBuilders[idx]
			if !ok {
				b = &toolCallBuilder{}
				toolCallBuilders[idx] = b
			}
			// Start event carries ID and name.
			if ev.Delta.ToolCallID != "" {
				b.id = ev.Delta.ToolCallID
			}
			if ev.Delta.ToolName != "" {
				b.name = ev.Delta.ToolName
			}
			// Delta carries partial input JSON.
			if ev.Delta.ToolInput != "" {
				b.inputBuf.WriteString(ev.Delta.ToolInput)
			}
			// Prefer Meta over deprecated ToolMeta.
			if len(ev.Delta.Meta) > 0 {
				b.meta = ev.Delta.Meta
			} else if len(ev.Delta.ToolMeta) > 0 {
				b.meta = ev.Delta.ToolMeta
			}

		case "usage":
			if ev.Usage != nil {
				usage.InputTokens += ev.Usage.InputTokens
				usage.OutputTokens += ev.Usage.OutputTokens
				usage.CacheCreation += ev.Usage.CacheCreation
				usage.CacheRead += ev.Usage.CacheRead
			}
			// Anthropic sends stop_reason on usage events (message_delta).
			if ev.StopReason != "" {
				stopReason = ev.StopReason
			}

		case "done":
			if ev.StopReason != "" {
				stopReason = ev.StopReason
			}

		case "error":
			streamErr = ev.Error
		}

		// Check context cancellation.
		if ctx.Err() != nil {
			streamErr = ctx.Err()
			break
		}
	}

	// Build the complete assistant message.
	var content []canonical.Content

	// Thinking blocks go first (before text) for correct round-trip ordering.
	if thinkingBuf.Len() > 0 {
		c := canonical.Content{
			Type:     "thinking",
			Thinking: thinkingBuf.String(),
		}
		if thinkingSig != "" {
			c.Meta = map[string]string{"thinking_signature": thinkingSig}
		}
		content = append(content, c)
	}

	if textBuf.Len() > 0 {
		content = append(content, canonical.Content{
			Type: "text",
			Text: textBuf.String(),
		})
	}

	// Add complete tool calls from providers that accumulate internally.
	content = append(content, completeToolCalls...)

	// Assemble tool calls from accumulated delta builders.
	for _, b := range toolCallBuilders {
		tc := b.build()
		content = append(content, canonical.Content{
			Type: "tool_call",
			ToolCall: &canonical.ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
				Meta:  tc.Meta,
			},
		})
	}

	return streamResult{
		Message: canonical.Message{
			Role:    "assistant",
			Content: content,
		},
		Usage:      usage,
		StopReason: stopReason,
		Error:      streamErr,
	}
}

// toolCallBuilder accumulates partial tool call data from stream deltas.
type toolCallBuilder struct {
	id       string
	name     string
	inputBuf strings.Builder
	meta     map[string]string // Provider metadata (e.g., Gemini thought_signature).
}

func (b *toolCallBuilder) build() canonical.ToolCall {
	input := b.inputBuf.String()
	if input == "" {
		input = "{}"
	}

	return canonical.ToolCall{
		ID:    b.id,
		Name:  b.name,
		Input: []byte(input),
		Meta:  b.meta,
	}
}

// streamError wraps an error with context about whether it happened
// before the stream started or mid-stream.
type streamError struct {
	err       error
	midStream bool // true if error occurred after receiving some data
}

func (e *streamError) Error() string {
	if e.midStream {
		return fmt.Sprintf("mid-stream error: %v", e.err)
	}
	return e.err.Error()
}

func (e *streamError) Unwrap() error { return e.err }
