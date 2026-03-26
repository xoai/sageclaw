package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

// --- Mock implementations ---

type mockAudioCodec struct{}

func (m *mockAudioCodec) DecodeOGGToPCM(ogg []byte, rate int) ([]byte, error) {
	// Return fake PCM data (double the size to simulate expansion).
	return make([]byte, len(ogg)*2), nil
}

func (m *mockAudioCodec) EncodePCMToOGG(pcm []byte, rate int) ([]byte, error) {
	// Return fake OGG data (half the size to simulate compression).
	return make([]byte, len(pcm)/2), nil
}

type mockAudioStore struct {
	files map[string][]byte
}

func newMockAudioStore() *mockAudioStore {
	return &mockAudioStore{files: make(map[string][]byte)}
}

func (m *mockAudioStore) Save(sessionID, msgID string, data []byte, ext string) (string, error) {
	path := sessionID + "/" + msgID + "." + ext
	m.files[path] = data
	return path, nil
}

func (m *mockAudioStore) Load(path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, nil
}

type mockLiveSessionPool struct {
	session provider.LiveSession
}

func (m *mockLiveSessionPool) GetOrCreate(ctx context.Context, sessionID string, cfg provider.LiveSessionConfig) (provider.LiveSession, error) {
	return m.session, nil
}

type mockLiveSession struct {
	events chan provider.LiveEvent
}

func (s *mockLiveSession) ID() string                                               { return "mock" }
func (s *mockLiveSession) Send(ctx context.Context, msg provider.LiveMessage) error  { return nil }
func (s *mockLiveSession) Receive() <-chan provider.LiveEvent                        { return s.events }
func (s *mockLiveSession) Close() error                                              { return nil }

// --- Tests ---

func TestCanVoice(t *testing.T) {
	reg := tool.NewRegistry()
	loop := NewLoop(
		Config{AgentID: "test", SystemPrompt: "test", VoiceEnabled: true},
		nil, reg, nil, nil, nil,
	)

	if loop.CanVoice() {
		t.Error("CanVoice should be false without LiveProvider/store")
	}

	// Codec is optional — CanVoice should be true with just provider + store.
	loop2 := NewLoop(
		Config{AgentID: "test", SystemPrompt: "test", VoiceEnabled: true},
		nil, reg, nil, nil, nil,
		WithLiveProvider(&mockLiveProvider{}),
		WithAudioStore(newMockAudioStore()),
	)

	if !loop2.CanVoice() {
		t.Error("CanVoice should be true with provider + store (no codec needed)")
	}
}

func TestRunVoice_BasicFlow(t *testing.T) {
	// Create a mock live session that returns audio + transcript.
	events := make(chan provider.LiveEvent, 10)
	events <- provider.LiveEvent{Type: "audio", Audio: make([]byte, 4800)} // 100ms at 24kHz
	events <- provider.LiveEvent{
		Type:       "transcript",
		Transcript: &provider.LiveTranscript{Direction: "output", Text: "Hello there!"},
	}
	events <- provider.LiveEvent{Type: "done"}

	mockSess := &mockLiveSession{events: events}
	pool := &mockLiveSessionPool{session: mockSess}
	store := newMockAudioStore()

	// Pre-populate input audio file.
	store.files["test-session/input.ogg"] = make([]byte, 100)

	reg := tool.NewRegistry()
	loop := NewLoop(
		Config{
			AgentID:      "test",
			SystemPrompt: "test",
			VoiceEnabled: true,
			VoiceModel:   "test-model",
		},
		nil, reg, nil, nil, nil,
		WithLiveProvider(&mockLiveProvider{}),
		WithAudioCodec(&mockAudioCodec{}),
		WithAudioStore(store),
	)

	history := []canonical.Message{
		{
			Role: "user",
			Content: []canonical.Content{{
				Type: "audio",
				Audio: &canonical.AudioSource{
					FilePath: "test-session/input.ogg",
					MimeType: "audio/ogg",
				},
			}},
		},
	}

	result := loop.RunVoice(context.Background(), "test-session", history, pool)

	if result.Error != nil {
		t.Fatalf("RunVoice error: %v", result.Error)
	}

	if len(result.Messages) == 0 {
		t.Fatal("expected at least one response message")
	}

	msg := result.Messages[0]
	if msg.Role != "assistant" {
		t.Errorf("role = %q, want assistant", msg.Role)
	}

	// Should have audio content.
	hasAudio := false
	hasText := false
	for _, c := range msg.Content {
		if c.Type == "audio" && c.Audio != nil {
			hasAudio = true
			if c.Audio.Transcript != "Hello there!" {
				t.Errorf("transcript = %q, want 'Hello there!'", c.Audio.Transcript)
			}
		}
		if c.Type == "text" && c.Text == "Hello there!" {
			hasText = true
		}
	}

	if !hasAudio {
		t.Error("response should contain audio content")
	}
	if !hasText {
		t.Error("response should contain text transcript")
	}
}

func TestRunVoice_NoAudioInHistory(t *testing.T) {
	pool := &mockLiveSessionPool{session: &mockLiveSession{events: make(chan provider.LiveEvent, 1)}}
	reg := tool.NewRegistry()
	loop := NewLoop(
		Config{AgentID: "test", SystemPrompt: "test", VoiceEnabled: true},
		nil, reg, nil, nil, nil,
		WithLiveProvider(&mockLiveProvider{}),
		WithAudioCodec(&mockAudioCodec{}),
		WithAudioStore(newMockAudioStore()),
	)

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	}

	result := loop.RunVoice(context.Background(), "test-session", history, pool)
	if result.Error == nil {
		t.Error("expected error when no audio in history")
	}
}

func TestRunVoice_ToolCallFlow(t *testing.T) {
	// Mock session: sends tool_call → expects tool result → sends audio + done.
	events := make(chan provider.LiveEvent, 10)
	events <- provider.LiveEvent{
		Type: "tool_call",
		ToolCall: &canonical.ToolCall{
			ID:    "call_1",
			Name:  "test_tool",
			Input: []byte(`{"q":"hello"}`),
		},
	}
	// After tool results are sent back, model responds with audio + transcript.
	// We pre-load these — the mock session ignores Send() calls.
	events <- provider.LiveEvent{Type: "audio", Audio: make([]byte, 4800)}
	events <- provider.LiveEvent{
		Type:       "transcript",
		Transcript: &provider.LiveTranscript{Direction: "output", Text: "Tool result processed."},
	}
	events <- provider.LiveEvent{Type: "done"}

	mockSess := &mockLiveSession{events: events}
	pool := &mockLiveSessionPool{session: mockSess}
	store := newMockAudioStore()
	store.files["test-session/input.ogg"] = make([]byte, 100)

	reg := tool.NewRegistry()
	// Register a test tool.
	reg.Register("test_tool", "A test tool", nil, func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		return &canonical.ToolResult{Content: "tool executed"}, nil
	})

	var toolCallSeen bool
	loop := NewLoop(
		Config{
			AgentID:      "test",
			SystemPrompt: "test",
			VoiceEnabled: true,
			VoiceModel:   "test-model",
		},
		nil, reg, nil, nil,
		func(e Event) {
			if e.Type == EventToolCall {
				toolCallSeen = true
			}
		},
		WithLiveProvider(&mockLiveProvider{}),
		WithAudioCodec(&mockAudioCodec{}),
		WithAudioStore(store),
	)

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{
			Type:  "audio",
			Audio: &canonical.AudioSource{FilePath: "test-session/input.ogg", MimeType: "audio/ogg"},
		}}},
	}

	result := loop.RunVoice(context.Background(), "test-session", history, pool)

	if result.Error != nil {
		t.Fatalf("RunVoice error: %v", result.Error)
	}
	if !toolCallSeen {
		t.Error("expected tool call event to be emitted")
	}
	if len(result.Messages) == 0 {
		t.Fatal("expected response messages")
	}
}

func TestPcmDurationMs(t *testing.T) {
	// 24000 samples at 24kHz = 1 second = 48000 bytes
	data := make([]byte, 48000)
	got := pcmDurationMs(data, 24000)
	if got != 1000 {
		t.Errorf("pcmDurationMs = %d, want 1000", got)
	}
}

// mockLiveProvider for agent tests.
type mockLiveProvider struct{}

func (m *mockLiveProvider) Name() string { return "mock" }
func (m *mockLiveProvider) OpenSession(ctx context.Context, cfg provider.LiveSessionConfig) (provider.LiveSession, error) {
	return &mockLiveSession{events: make(chan provider.LiveEvent, 1)}, nil
}
