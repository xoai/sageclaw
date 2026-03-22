package openai

import (
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// --- Request translation: canonical → OpenAI ---

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content"`               // string or null
	ToolCalls  []chatToolCall  `json:"tool_calls,omitempty"`  // assistant only
	ToolCallID string          `json:"tool_call_id,omitempty"` // tool role only
	Name       string          `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type chatTool struct {
	Type     string           `json:"type"` // "function"
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToOpenAIRequest converts a canonical request to OpenAI Chat Completions format.
func ToOpenAIRequest(req *canonical.Request) ([]byte, error) {
	cr := chatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}

	if req.Temperature > 0 {
		t := req.Temperature
		cr.Temperature = &t
	}

	// System prompt as first message.
	if req.System != "" {
		cr.Messages = append(cr.Messages, chatMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Tools.
	for _, t := range req.Tools {
		cr.Tools = append(cr.Tools, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	// Messages.
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			// Check if this contains tool results.
			for _, c := range msg.Content {
				if c.ToolResult != nil {
					cr.Messages = append(cr.Messages, chatMessage{
						Role:       "tool",
						Content:    c.ToolResult.Content,
						ToolCallID: c.ToolResult.ToolCallID,
					})
					continue
				}
				if c.Type == "text" {
					cr.Messages = append(cr.Messages, chatMessage{
						Role:    "user",
						Content: c.Text,
					})
				}
			}

		case "assistant":
			am := chatMessage{Role: "assistant"}
			var textParts string
			var toolCalls []chatToolCall

			for _, c := range msg.Content {
				if c.Type == "text" {
					textParts += c.Text
				}
				if c.ToolCall != nil {
					toolCalls = append(toolCalls, chatToolCall{
						ID:   c.ToolCall.ID,
						Type: "function",
						Function: chatFunction{
							Name:      c.ToolCall.Name,
							Arguments: string(c.ToolCall.Input),
						},
					})
				}
			}

			if textParts != "" {
				am.Content = textParts
			}
			if len(toolCalls) > 0 {
				am.ToolCalls = toolCalls
				if am.Content == nil {
					am.Content = nil // OpenAI wants null content when tool_calls present
				}
			}
			cr.Messages = append(cr.Messages, am)

		case "tool":
			for _, c := range msg.Content {
				if c.ToolResult != nil {
					cr.Messages = append(cr.Messages, chatMessage{
						Role:       "tool",
						Content:    c.ToolResult.Content,
						ToolCallID: c.ToolResult.ToolCallID,
					})
				}
			}
		}
	}

	return json.Marshal(cr)
}

// --- Response translation: OpenAI → canonical ---

type chatResponse struct {
	ID      string         `json:"id"`
	Choices []chatChoice   `json:"choices"`
	Usage   chatUsage      `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatRespMsg `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatRespMsg struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// FromOpenAIResponse converts an OpenAI response to canonical format.
func FromOpenAIResponse(data []byte) (*canonical.Response, error) {
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := cr.Choices[0]
	var content []canonical.Content

	if choice.Message.Content != nil && *choice.Message.Content != "" {
		content = append(content, canonical.Content{Type: "text", Text: *choice.Message.Content})
	}

	for _, tc := range choice.Message.ToolCalls {
		content = append(content, canonical.Content{
			Type: "tool_call",
			ToolCall: &canonical.ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			},
		})
	}

	stopReason := mapFinishReason(choice.FinishReason)

	return &canonical.Response{
		ID: cr.ID,
		Messages: []canonical.Message{
			{Role: "assistant", Content: content},
		},
		Usage: canonical.Usage{
			InputTokens:  cr.Usage.PromptTokens,
			OutputTokens: cr.Usage.CompletionTokens,
		},
		StopReason: stopReason,
	}, nil
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
