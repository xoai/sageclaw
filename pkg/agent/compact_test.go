package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// mockMemEngine implements memory.MemoryEngine for testing.
type mockMemEngine struct {
	written []string
}

func (m *mockMemEngine) Write(_ context.Context, content, title string, tags []string) (string, error) {
	m.written = append(m.written, title+": "+content)
	return "mock-id", nil
}
func (m *mockMemEngine) Search(_ context.Context, _ string, _ memory.SearchOptions) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockMemEngine) List(_ context.Context, _ []string, _, _ int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockMemEngine) Delete(_ context.Context, _ string) error { return nil }

// mockLLM returns a canned response.
func mockLLM(response string) LLMCaller {
	return func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		return response, nil
	}
}

func TestCompactionManager_SkipsWhenDisabled(t *testing.T) {
	cm := NewCompactionManager(nil, nil)

	var msgs []canonical.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}})
	}

	cfg := CompactionConfig{Enabled: false}
	result := cm.TryCompact(context.Background(), "test-session-1234", msgs, cfg, 200000)
	if result != nil {
		t.Error("should skip when disabled")
	}
}

func TestCompactionManager_SkipsWhenBelowThreshold(t *testing.T) {
	cm := NewCompactionManager(nil, nil)

	var msgs []canonical.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}})
	}

	cfg := DefaultCompactionConfig()
	result := cm.TryCompact(context.Background(), "test-session-1234", msgs, cfg, 200000)
	if result != nil {
		t.Error("should skip when below threshold")
	}
}

func TestCompactionManager_CompactsWhenAboveThreshold(t *testing.T) {
	mem := &mockMemEngine{}
	llm := mockLLM(`[{"title":"Test fact","content":"Important information from the conversation"}]`)
	cm := NewCompactionManager(mem, llm)

	var msgs []canonical.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello what is going on"}}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "everything is fine"}}},
		)
	}

	cfg := DefaultCompactionConfig()
	result := cm.TryCompact(context.Background(), "test-session-1234", msgs, cfg, 200000)
	if result == nil {
		t.Fatal("should compact when above threshold (120 messages > 50)")
	}

	if len(result) >= len(msgs) {
		t.Errorf("compacted result should have fewer messages: got %d, original %d", len(result), len(msgs))
	}

	// First message should be the summary.
	if result[0].Role != "assistant" {
		t.Error("first message should be assistant (summary)")
	}
	if !strings.Contains(result[0].Content[0].Text, "summary") {
		t.Error("first message should contain summary marker")
	}

	// Memory should have been flushed.
	if len(mem.written) == 0 {
		t.Error("expected facts to be written to memory")
	}
}

func TestCompactionManager_NonBlocking(t *testing.T) {
	cm := NewCompactionManager(nil, nil)

	// Manually lock the session.
	lock := cm.sessionLock("test-session-1234")
	lock.Lock()

	var msgs []canonical.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}})
	}

	cfg := DefaultCompactionConfig()
	result := cm.TryCompact(context.Background(), "test-session-1234", msgs, cfg, 200000)
	if result != nil {
		t.Error("should skip when session is already locked")
	}

	lock.Unlock()
}

func TestFallbackSummary(t *testing.T) {
	cm := NewCompactionManager(nil, nil)
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "What is the weather?"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "It's sunny."}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Thanks!"}}},
	}

	summary := cm.fallbackSummary(msgs)
	if !strings.Contains(summary, "weather") {
		t.Error("fallback summary should include user messages")
	}
	if !strings.Contains(summary, "2 turns") {
		t.Error("fallback summary should include turn count")
	}
}
