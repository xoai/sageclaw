package agent

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// mockMemEngineForReview extends mockMemEngine with thread-safe write tracking.
type mockMemEngineForReview struct {
	mu      sync.Mutex
	written []reviewWritten
}

type reviewWritten struct {
	title   string
	content string
	tags    []string
}

func (m *mockMemEngineForReview) Write(_ context.Context, content, title string, tags []string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.written = append(m.written, reviewWritten{title: title, content: content, tags: tags})
	return "mock-id", nil
}
func (m *mockMemEngineForReview) Search(_ context.Context, _ string, _ memory.SearchOptions) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockMemEngineForReview) List(_ context.Context, _ []string, _, _ int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockMemEngineForReview) Delete(_ context.Context, _ string) error { return nil }

func (m *mockMemEngineForReview) getWritten() []reviewWritten {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]reviewWritten, len(m.written))
	copy(cp, m.written)
	return cp
}

func testHistory(n int) []canonical.Message {
	var msgs []canonical.Message
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
		)
	}
	return msgs
}

func TestBackgroundReview_TurnCounterIncrements(t *testing.T) {
	mem := &mockMemEngineForReview{}
	llm := mockLLM(`[]`)
	br := NewBackgroundReviewer(mem, llm, 10)

	history := testHistory(5)
	for i := 0; i < 5; i++ {
		br.OnUserTurn("test-session-12345678", history, 1)
	}

	count := atomic.LoadInt32(&br.turnCount)
	if count != 5 {
		t.Errorf("turn count should be 5, got %d", count)
	}
}

func TestBackgroundReview_TriggersAtExactInterval(t *testing.T) {
	mem := &mockMemEngineForReview{}
	var llmCallCount atomic.Int32
	llm := func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		llmCallCount.Add(1)
		return `[{"title":"pref","content":"user likes Go","type":"memory"}]`, nil
	}
	br := NewBackgroundReviewer(mem, llm, 5)

	history := testHistory(5)

	// Turns 1-4: no review should fire.
	for i := 0; i < 4; i++ {
		br.OnUserTurn("test-session-12345678", history, 1)
	}
	time.Sleep(50 * time.Millisecond) // Let any goroutines finish.
	if llmCallCount.Load() != 0 {
		t.Errorf("should not fire before interval (4 turns), got %d calls", llmCallCount.Load())
	}

	// Turn 5: review should fire.
	br.OnUserTurn("test-session-12345678", history, 1)
	time.Sleep(200 * time.Millisecond) // Wait for goroutine.
	if llmCallCount.Load() != 1 {
		t.Errorf("should fire at interval (5 turns), got %d calls", llmCallCount.Load())
	}

	// Turn 6-9: no review.
	for i := 0; i < 4; i++ {
		br.OnUserTurn("test-session-12345678", history, 1)
	}
	time.Sleep(50 * time.Millisecond)
	if llmCallCount.Load() != 1 {
		t.Errorf("should not fire between intervals, got %d calls", llmCallCount.Load())
	}

	// Turn 10: review fires again.
	br.OnUserTurn("test-session-12345678", history, 1)
	time.Sleep(200 * time.Millisecond)
	if llmCallCount.Load() != 2 {
		t.Errorf("should fire at second interval (10 turns), got %d calls", llmCallCount.Load())
	}
}

func TestBackgroundReview_Debounce(t *testing.T) {
	mem := &mockMemEngineForReview{}
	var llmCallCount atomic.Int32
	llm := func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		llmCallCount.Add(1)
		time.Sleep(300 * time.Millisecond) // Simulate slow LLM call.
		return `[]`, nil
	}
	br := NewBackgroundReviewer(mem, llm, 1) // Review every turn for testing.

	history := testHistory(5)

	// Fire 3 turns rapidly — only 1 review should run (others debounced).
	br.OnUserTurn("test-session-12345678", history, 1)
	time.Sleep(10 * time.Millisecond) // Let goroutine start.
	br.OnUserTurn("test-session-12345678", history, 1)
	br.OnUserTurn("test-session-12345678", history, 1)

	time.Sleep(500 * time.Millisecond) // Wait for completion.
	if llmCallCount.Load() != 1 {
		t.Errorf("debounce should prevent concurrent reviews, got %d calls", llmCallCount.Load())
	}
}

func TestBackgroundReview_DisabledWhenIntervalZero(t *testing.T) {
	mem := &mockMemEngineForReview{}
	var llmCallCount atomic.Int32
	llm := func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		llmCallCount.Add(1)
		return `[]`, nil
	}
	br := NewBackgroundReviewer(mem, llm, 0)

	history := testHistory(5)
	for i := 0; i < 20; i++ {
		br.OnUserTurn("test-session-12345678", history, 1)
	}
	time.Sleep(100 * time.Millisecond)
	if llmCallCount.Load() != 0 {
		t.Errorf("should never fire when interval=0, got %d calls", llmCallCount.Load())
	}
}

func TestBackgroundReview_MalformedJSONFallback(t *testing.T) {
	mem := &mockMemEngineForReview{}
	llm := func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		return "This is not valid JSON at all", nil
	}
	br := NewBackgroundReviewer(mem, llm, 1)

	history := testHistory(5)
	br.OnUserTurn("test-session-12345678", history, 1)
	time.Sleep(200 * time.Millisecond)

	written := mem.getWritten()
	if len(written) != 1 {
		t.Fatalf("malformed JSON should store 1 fallback entry, got %d", len(written))
	}
	// Check tags are review-specific (not compaction).
	hasTags := false
	for _, tag := range written[0].tags {
		if tag == "review" {
			hasTags = true
		}
		if tag == "compaction" {
			t.Error("fallback tags should be 'review', not 'compaction'")
		}
	}
	if !hasTags {
		t.Error("fallback entry should have 'review' tag")
	}
}

func TestBackgroundReview_StoresExtractedMemories(t *testing.T) {
	mem := &mockMemEngineForReview{}
	llm := func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		return `[
			{"title":"User prefers Go","content":"User strongly prefers Go over Python","type":"memory"},
			{"title":"Project uses SQLite","content":"All storage uses embedded SQLite","type":"memory"}
		]`, nil
	}
	br := NewBackgroundReviewer(mem, llm, 1)

	history := testHistory(5)
	br.OnUserTurn("test-session-12345678", history, 1)
	time.Sleep(200 * time.Millisecond)

	written := mem.getWritten()
	if len(written) != 2 {
		t.Fatalf("should store 2 memory entries, got %d", len(written))
	}
	if written[0].title != "User prefers Go" {
		t.Errorf("unexpected title: %s", written[0].title)
	}
}

func TestBackgroundReview_ProcedurePromptOnlyForComplexTasks(t *testing.T) {
	var receivedPrompt string
	llm := func(_ context.Context, systemPrompt string, _ []canonical.Message) (string, error) {
		receivedPrompt = systemPrompt
		return `[]`, nil
	}
	mem := &mockMemEngineForReview{}
	br := NewBackgroundReviewer(mem, llm, 1)

	history := testHistory(5)

	// Simple task (2 iterations) — no procedure prompt.
	br.OnUserTurn("test-session-12345678", history, 2)
	time.Sleep(200 * time.Millisecond)
	if receivedPrompt == "" {
		t.Fatal("should have received a prompt")
	}
	if strings.Contains(receivedPrompt, "procedure") {
		t.Error("simple task (2 iterations) should NOT include procedure extraction")
	}

	// Complex task (7 iterations) — procedure prompt included.
	receivedPrompt = ""
	br.OnUserTurn("test-session-12345678", history, 7)
	time.Sleep(200 * time.Millisecond)
	if !strings.Contains(receivedPrompt, "procedure") {
		t.Error("complex task (7 iterations) SHOULD include procedure extraction")
	}
}

// mockConfidenceEngine extends mockMemEngineForReview with ConfidenceWriter.
type mockConfidenceEngine struct {
	mockMemEngineForReview
	confWrites []confWrite
}

type confWrite struct {
	title      string
	content    string
	tags       []string
	confidence float64
}

func (m *mockConfidenceEngine) WriteWithConfidence(_ context.Context, content, title string, tags []string, conf float64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confWrites = append(m.confWrites, confWrite{title: title, content: content, tags: tags, confidence: conf})
	return "mock-id", nil
}

func (m *mockConfidenceEngine) UpdateConfidence(_ context.Context, _ string, _ float64) error {
	return nil
}

func (m *mockConfidenceEngine) BumpConfidence(_ context.Context, _ string, _, _ float64) error {
	return nil
}

func (m *mockConfidenceEngine) getConfWrites() []confWrite {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]confWrite, len(m.confWrites))
	copy(cp, m.confWrites)
	return cp
}

func TestBackgroundReview_ProcedureStoredWithConfidence(t *testing.T) {
	mem := &mockConfidenceEngine{}
	llm := func(_ context.Context, _ string, _ []canonical.Message) (string, error) {
		return `[
			{"title":"[PROC] Debug races","content":"TRIGGER: flaky tests\nSTEPS:\n1. go test -race","type":"procedure"},
			{"title":"User pref","content":"Prefers verbose output","type":"memory"}
		]`, nil
	}
	br := NewBackgroundReviewer(mem, llm, 1)

	history := testHistory(5)
	br.OnUserTurn("test-session-12345678", history, 7) // Complex task.
	time.Sleep(300 * time.Millisecond)

	// Procedure should be written via WriteWithConfidence with 0.5.
	confWrites := mem.getConfWrites()
	if len(confWrites) < 1 {
		t.Fatalf("expected at least 1 confidence write (procedure), got %d", len(confWrites))
	}

	procWrite := confWrites[0]
	if procWrite.confidence != 0.5 {
		t.Errorf("procedure initial confidence should be 0.5, got %f", procWrite.confidence)
	}
	if procWrite.title != "[PROC] Debug races" {
		t.Errorf("unexpected procedure title: %s", procWrite.title)
	}

	// Check tags include both self-learning and procedure.
	hasSL, hasProc := false, false
	for _, tag := range procWrite.tags {
		if tag == "self-learning" {
			hasSL = true
		}
		if tag == "procedure" {
			hasProc = true
		}
	}
	if !hasSL || !hasProc {
		t.Errorf("procedure tags should include self-learning and procedure, got %v", procWrite.tags)
	}

	// Memory entry should be written via regular Write (not confidence).
	regularWrites := mem.getWritten()
	if len(regularWrites) < 1 {
		t.Fatalf("expected at least 1 regular write (memory), got %d", len(regularWrites))
	}
}

func TestBackgroundReview_SecurityPromptPresent(t *testing.T) {
	var receivedPrompt string
	llm := func(_ context.Context, systemPrompt string, _ []canonical.Message) (string, error) {
		receivedPrompt = systemPrompt
		return `[]`, nil
	}
	mem := &mockMemEngineForReview{}
	br := NewBackgroundReviewer(mem, llm, 1)

	history := testHistory(5)
	br.OnUserTurn("test-session-12345678", history, 7) // Complex task — triggers procedure prompt.
	time.Sleep(200 * time.Millisecond)

	// Verify security guardrails are in the prompt.
	if !strings.Contains(receivedPrompt, "destructive") {
		t.Error("procedure prompt should warn against destructive commands")
	}
	if !strings.Contains(receivedPrompt, "secrets") {
		t.Error("procedure prompt should warn against including secrets")
	}
	if !strings.Contains(receivedPrompt, "rm -rf") {
		t.Error("procedure prompt should explicitly mention rm -rf")
	}
}

func TestBackgroundReview_Integration(t *testing.T) {
	apiKey := os.Getenv("SAGECLAW_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("SAGECLAW_TEST_API_KEY not set — skipping integration test")
	}
	t.Skip("integration test placeholder — requires provider wiring")
}
