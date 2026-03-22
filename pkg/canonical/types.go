package canonical

import "encoding/json"

// Message represents a single message in a conversation.
type Message struct {
	Role    string    `json:"role"`    // "user", "assistant", "tool"
	Content []Content `json:"content"` // Always an array (sage-router convention)
}

// Content represents a content block within a message.
type Content struct {
	Type       string       `json:"type"`
	Text       string       `json:"text,omitempty"`
	ToolCall   *ToolCall    `json:"tool_call,omitempty"`
	ToolResult *ToolResult  `json:"tool_result,omitempty"`
	Thinking   string       `json:"thinking,omitempty"`
	Source     *ImageSource `json:"source,omitempty"`
}

// ToolCall represents an LLM-initiated tool invocation.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// ToolDef describes a tool available to the LLM.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ImageSource describes an inline image.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`
}

// Request is the canonical LLM request format.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	System      string    `json:"system,omitempty"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Response is the canonical LLM response format.
type Response struct {
	ID         string    `json:"id"`
	Messages   []Message `json:"messages"`
	Usage      Usage     `json:"usage"`
	StopReason string    `json:"stop_reason"` // "end_turn", "tool_use", "max_tokens"
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens   int `json:"input_tokens"`
	OutputTokens  int `json:"output_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens,omitempty"`
	CacheRead     int `json:"cache_read_input_tokens,omitempty"`
}
