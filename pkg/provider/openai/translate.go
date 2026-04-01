package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// --- Request translation: canonical → OpenAI ---

type chatRequest struct {
	Model               string              `json:"model"`
	Messages            []chatMessage       `json:"messages"`
	Tools               []chatTool          `json:"tools,omitempty"`
	MaxTokens           int                 `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
	Temperature         *float64            `json:"temperature,omitempty"`
	ReasoningEffort     string              `json:"reasoning_effort,omitempty"` // o-series: "low", "medium", "high".
	Stream              bool                `json:"stream,omitempty"`
	StreamOptions       *chatStreamOptions  `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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
	Type     string       `json:"type"`  // "function"
	Index    int          `json:"index"` // Set in streaming deltas.
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

// isOSeries returns true for OpenAI reasoning models (o1, o3, o4-mini, etc.).
func isOSeries(model string) bool {
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// usesLegacyMaxTokens returns true for older OpenAI models that still require
// the deprecated max_tokens parameter instead of max_completion_tokens.
// New models (gpt-5+, o-series) all use max_completion_tokens.
func usesLegacyMaxTokens(model string) bool {
	for _, prefix := range []string{"gpt-4o", "gpt-4-", "gpt-4.", "gpt-3", "chatgpt-4o"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	// Exact match for "gpt-4" without suffix.
	if model == "gpt-4" {
		return true
	}
	return false
}

// ToOpenAIRequest converts a canonical request to OpenAI Chat Completions format.
func ToOpenAIRequest(req *canonical.Request) ([]byte, error) {
	cr := chatRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}

	// Request usage data in streaming responses.
	// Some providers (Ollama) don't support stream_options — they set "no_stream_options" in Options.
	if req.Stream && (req.Options == nil || req.Options["no_stream_options"] == nil) {
		cr.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}

	if isOSeries(req.Model) {
		// Reasoning models: max_completion_tokens, reasoning_effort, no temperature.
		cr.MaxCompletionTokens = req.MaxTokens
		if level, _ := req.Options["thinking_level"].(string); level != "" {
			cr.ReasoningEffort = level
		}
	} else if usesLegacyMaxTokens(req.Model) {
		// Legacy models (gpt-4o, gpt-4, gpt-3.5): max_tokens + temperature.
		cr.MaxTokens = req.MaxTokens
		if req.Temperature > 0 {
			t := req.Temperature
			cr.Temperature = &t
		}
	} else {
		// All other models (gpt-5+, future models): max_completion_tokens + temperature.
		cr.MaxCompletionTokens = req.MaxTokens
		if req.Temperature > 0 {
			t := req.Temperature
			cr.Temperature = &t
		}
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
	PromptTokens           int                     `json:"prompt_tokens"`
	CompletionTokens       int                     `json:"completion_tokens"`
	TotalTokens            int                     `json:"total_tokens"`
	PromptTokensDetails    *promptTokensDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// promptTokensDetails contains the breakdown of prompt token usage.
// OpenAI returns this for models that support prompt caching.
type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
	AudioTokens  int `json:"audio_tokens"`
}

// completionTokensDetails contains the breakdown of completion token usage.
// OpenAI returns this for o-series models with reasoning tokens.
type completionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
	AudioTokens     int `json:"audio_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
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
		Usage: openAIUsageToCanonical(cr.Usage),
		StopReason: stopReason,
	}, nil
}

// openAIUsageToCanonical converts OpenAI's usage (with optional detail breakdowns)
// to canonical.Usage. Extracts cached_tokens and reasoning_tokens when available.
func openAIUsageToCanonical(u chatUsage) canonical.Usage {
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
