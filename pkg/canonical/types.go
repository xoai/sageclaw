package canonical

import "encoding/json"

// MessageAnnotations holds pipeline metadata for a message.
// Never serialized to providers or storage (json:"-" on Message field).
type MessageAnnotations struct {
	Iteration     int    // Loop iteration that produced this message.
	TokenEstimate int    // Cached token count. 0 = not yet computed.
	Snippable     bool   // True if all tool results are read-only.
	Snipped       bool   // True if content was replaced by snip marker.
	OverflowPath  string // Disk path if result was persisted to overflow.
	CollapsedFrom int    // Original message index before projection.
	Summary       string // One-line tool summary (generated async, may be empty).
}

// Message represents a single message in a conversation.
type Message struct {
	Role        string              `json:"role"`    // "user", "assistant", "tool"
	Content     []Content           `json:"content"` // Always an array (sage-router convention)
	Annotations *MessageAnnotations `json:"-"`       // Pipeline metadata, never serialized.
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
	ToolCallID string            `json:"tool_call_id,omitempty"` // For tool_call start delta.
	ToolName   string            `json:"tool_name,omitempty"`    // For tool_call start delta.
	ToolInput  string            `json:"tool_input,omitempty"`   // For tool_call input_json_delta (partial JSON).
	ToolMeta   map[string]string `json:"tool_meta,omitempty"`    // Deprecated: use Meta instead.
	Meta       map[string]string `json:"meta,omitempty"`         // Provider metadata (e.g., thought_signature).
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
	Meta  map[string]string `json:"meta,omitempty"` // Provider-specific metadata (e.g., Gemini thought_signature).
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

// SystemPart represents a segment of the system prompt with caching intent.
// Providers that support prompt caching use the Cacheable flag to apply
// their specific caching mechanism (Anthropic cache_control, Gemini
// cachedContent, OpenAI automatic prefix caching).
type SystemPart struct {
	Content   string `json:"content"`
	Cacheable bool   `json:"cacheable"`
}

// JoinSystemParts concatenates all parts into a single string,
// separated by double newlines. Used for backward compatibility
// with providers that don't support SystemParts.
func JoinSystemParts(parts []SystemPart) string {
	if len(parts) == 0 {
		return ""
	}
	var total int
	for _, p := range parts {
		total += len(p.Content) + 2
	}
	buf := make([]byte, 0, total)
	for i, p := range parts {
		if i > 0 {
			buf = append(buf, '\n', '\n')
		}
		buf = append(buf, p.Content...)
	}
	return string(buf)
}

// Request is the canonical LLM request format.
type Request struct {
	Model            string         `json:"model"`
	Messages         []Message      `json:"messages"`
	System           string         `json:"system,omitempty"`
	SystemParts      []SystemPart   `json:"system_parts,omitempty"` // Structured system prompt for provider-specific caching.
	Tools            []ToolDef      `json:"tools,omitempty"`
	MaxTokens        int            `json:"max_tokens,omitempty"`
	Temperature      float64        `json:"temperature,omitempty"`
	Stream           bool           `json:"stream,omitempty"`
	Options          map[string]any `json:"options,omitempty"` // Provider-specific options (grounding, code_execution, thinking_level, etc.).
	SystemPromptSize int            `json:"-"`                 // Estimated tokens in system prompt (for diagnostics, not sent to LLM).
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
	InputTokens    int  `json:"input_tokens"`
	OutputTokens   int  `json:"output_tokens"`
	CacheCreation  int  `json:"cache_creation_input_tokens,omitempty"`
	CacheRead      int  `json:"cache_read_input_tokens,omitempty"`
	ThinkingTokens int  `json:"thinking_tokens,omitempty"` // Tokens used for extended thinking/reasoning.
	Estimated      bool `json:"estimated,omitempty"`        // True when tokens were estimated (not reported by provider).
}
