package provider

import (
	"context"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// StreamEvent represents a streaming response event from an LLM provider.
type StreamEvent struct {
	Type       string             // "content_delta", "tool_call", "usage", "done", "error"
	Delta      *canonical.Content // For content deltas and tool call deltas.
	Usage      *canonical.Usage   // For usage updates.
	Error      error              // For errors.
	Index      int                // Block index for tool call accumulation.
	StopReason string             // Set on "done" events (e.g. "end_turn", "tool_use").
}

// Provider defines the interface for LLM providers.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error)
	ChatStream(ctx context.Context, req *canonical.Request) (<-chan StreamEvent, error)
}

// ModelLister is an optional interface for providers that can list available models.
type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// Capability constants for ProviderCapabilities.
const (
	CapVision   = "vision"    // Can analyze images.
	CapDocument = "document"  // Can analyze documents (PDF, etc.).
	CapImageGen = "image_gen" // Can generate images.
	CapTTS      = "tts"       // Can generate speech from text.
)

// ProviderCapabilities is an optional interface for providers that support
// specific capabilities beyond basic chat (e.g. vision, document analysis,
// image generation).
type ProviderCapabilities interface {
	Supports(cap string) bool
}

// ProviderSupports checks whether a provider supports a given capability.
// Returns false if the provider does not implement ProviderCapabilities.
func ProviderSupports(p Provider, cap string) bool {
	if pc, ok := p.(ProviderCapabilities); ok {
		return pc.Supports(cap)
	}
	return false
}

// --- Live (bidirectional streaming) provider types ---

// LiveMessage is sent from client to a live session.
type LiveMessage struct {
	Audio       []byte                // Audio data (PCM, OGG, or other supported format).
	AudioMime   string                // MIME type of Audio (e.g. "audio/ogg", "audio/pcm;rate=16000"). Default: "audio/pcm;rate=16000".
	Text        string                // Text input (for mixed text+audio mode).
	ToolResults []canonical.ToolResult // Responses to tool calls from the model.
	ToolNames   map[string]string     // Maps ToolCallID → function name for tool responses.
}

// LiveEvent is received from a live session.
type LiveEvent struct {
	Type       string               // "audio", "text", "tool_call", "transcript", "usage", "go_away", "done", "error"
	Audio      []byte               // Raw PCM audio chunk.
	Text       string               // Text content or transcript text.
	ToolCall   *canonical.ToolCall  // Tool call request from model.
	Transcript *LiveTranscript      // Input or output transcript.
	Usage      *canonical.Usage     // Token usage update.
	Error      error                // Error details.
}

// LiveTranscript represents a speech-to-text transcript from the live session.
type LiveTranscript struct {
	Direction string // "input" or "output"
	Text      string
}

// LiveSessionConfig configures a new live session.
type LiveSessionConfig struct {
	Model              string              // Model ID (e.g. "gemini-2.5-flash-native-audio-preview-12-2025").
	SystemPrompt       string              // System instruction for the session.
	Tools              []canonical.ToolDef  // Available tools.
	VoiceName          string              // Voice preset (e.g. "Kore", "Sadaltager").
	LanguageCode       string              // BCP-47 language code (e.g. "en-US", "vi-VN"). Empty = auto-detect.
	ResponseModalities []string            // e.g. ["AUDIO"], ["TEXT"], ["AUDIO", "TEXT"].
}

// LiveSession represents an active bidirectional streaming session.
type LiveSession interface {
	// Send sends a message (audio, text, or tool results) to the model.
	Send(ctx context.Context, msg LiveMessage) error

	// Receive returns a channel that emits events from the model.
	// The channel is closed when the session ends.
	Receive() <-chan LiveEvent

	// Close terminates the session and releases resources.
	Close() error

	// ID returns a unique identifier for this session.
	ID() string
}

// LiveProvider creates live bidirectional streaming sessions.
type LiveProvider interface {
	Name() string
	OpenSession(ctx context.Context, cfg LiveSessionConfig) (LiveSession, error)
}
