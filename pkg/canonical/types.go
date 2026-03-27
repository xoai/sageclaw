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
	Audio      *AudioSource `json:"audio,omitempty"` // type: "audio"

	// Stream delta fields — used during streaming accumulation only.
	ToolCallID string `json:"tool_call_id,omitempty"` // For tool_call start delta.
	ToolName   string `json:"tool_name,omitempty"`    // For tool_call start delta.
	ToolInput  string `json:"tool_input,omitempty"`   // For tool_call input_json_delta (partial JSON).
}

// AudioSource describes an audio file reference.
type AudioSource struct {
	FilePath   string `json:"file_path"`             // Path on disk (e.g. "data/audio/{session}/{msg}.ogg")
	MimeType   string `json:"mime_type"`              // "audio/ogg", "audio/pcm", etc.
	DurationMs int    `json:"duration_ms,omitempty"`  // Duration in milliseconds
	Transcript string `json:"transcript,omitempty"`   // STT transcript if available
	SampleRate int    `json:"sample_rate,omitempty"`  // Sample rate in Hz (e.g. 16000, 24000)
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
	Model            string    `json:"model"`
	Messages         []Message `json:"messages"`
	System           string    `json:"system,omitempty"`
	Tools            []ToolDef `json:"tools,omitempty"`
	MaxTokens        int       `json:"max_tokens,omitempty"`
	Temperature      float64   `json:"temperature,omitempty"`
	Stream           bool      `json:"stream,omitempty"`
	SystemPromptSize int       `json:"-"` // Estimated tokens in system prompt (for diagnostics, not sent to LLM).
}

// Response is the canonical LLM response format.
type Response struct {
	ID         string    `json:"id"`
	Messages   []Message `json:"messages"`
	Usage      Usage     `json:"usage"`
	StopReason string    `json:"stop_reason"` // "end_turn", "tool_use", "max_tokens"
}

// HasAudio returns true if the message contains at least one audio content block.
func HasAudio(msg Message) bool {
	for _, c := range msg.Content {
		if c.Type == "audio" && c.Audio != nil {
			return true
		}
	}
	return false
}

// ExtractAudio returns the first audio source from a message, or nil.
func ExtractAudio(msg Message) *AudioSource {
	for _, c := range msg.Content {
		if c.Type == "audio" && c.Audio != nil {
			return c.Audio
		}
	}
	return nil
}

// MessagesHaveAudio returns true if any message in the slice contains audio.
func MessagesHaveAudio(msgs []Message) bool {
	for _, m := range msgs {
		if HasAudio(m) {
			return true
		}
	}
	return false
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens   int `json:"input_tokens"`
	OutputTokens  int `json:"output_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens,omitempty"`
	CacheRead     int `json:"cache_read_input_tokens,omitempty"`
}
