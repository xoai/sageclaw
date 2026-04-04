package agent

import (
	"context"
	"os"
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

func TestCompactionManager_TryCompactWithBudget_SkipsBelowThreshold(t *testing.T) {
	mem := &mockMemEngine{}
	llm := mockLLM(`[]`)
	cm := NewCompactionManager(mem, llm)

	budget := NewContextBudget(200000, 8192)

	// Small history — should not compact.
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	}

	result := cm.TryCompactWithBudget(context.Background(), "test-session-1234", msgs, budget)
	if result != nil {
		t.Error("should skip when budget usage is well below 1.0")
	}
}

func TestCompactionManager_TryCompactWithBudget_CompactsWhenOver(t *testing.T) {
	mem := &mockMemEngine{}
	llm := mockLLM(`[{"title":"Fact","content":"Important info"}]`)
	cm := NewCompactionManager(mem, llm)

	// Create a budget with a tiny history budget so messages exceed it.
	budget := &ContextBudget{
		contextWindow:   200000,
		responseReserve: 8192,
		overheadTokens:  190000, // Leaves very little for history.
		historyBudget:   100,    // Extremely tiny budget.
		calibrated:      true,
	}

	// Create enough messages to exceed the tiny 100-token budget.
	var msgs []canonical.Message
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: strings.Repeat("question about important topic ", 10)}}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: strings.Repeat("answer with detailed explanation ", 10)}}},
		)
	}

	result := cm.TryCompactWithBudget(context.Background(), "test-session-1234", msgs, budget)
	if result == nil {
		t.Fatal("should compact when budget usage exceeds 1.0")
	}
	if len(result) >= len(msgs) {
		t.Errorf("compacted should have fewer messages: %d vs %d", len(result), len(msgs))
	}
}

func TestCompactionManager_TryCompactWithBudget_NilBudget(t *testing.T) {
	cm := NewCompactionManager(nil, nil)
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
	}
	result := cm.TryCompactWithBudget(context.Background(), "test-session-1234", msgs, nil)
	if result != nil {
		t.Error("should return nil for nil budget")
	}
}

// TestIterativeCompaction_PromptIncludesPreviousSummary verifies that on a
// second compaction, the summarization prompt receives the previous summary
// in the messages, enabling iterative updates rather than replacement.
func TestIterativeCompaction_PromptIncludesPreviousSummary(t *testing.T) {
	mem := &mockMemEngine{}

	// Track what messages the LLM receives on each call.
	// Flow per compaction: call 1 = memoryFlush, call 2 = summarize.
	var llmCalls [][]canonical.Message
	var llmPrompts []string
	llm := func(_ context.Context, systemPrompt string, msgs []canonical.Message) (string, error) {
		copied := make([]canonical.Message, len(msgs))
		copy(copied, msgs)
		llmCalls = append(llmCalls, copied)
		llmPrompts = append(llmPrompts, systemPrompt)

		if strings.Contains(systemPrompt, "Extract important facts") {
			// Memory flush call.
			return `[{"title":"Fact","content":"info"}]`, nil
		}
		// Summarization call — check if this is first or second compaction
		// by looking for a previous summary in the input.
		hasPreviousSummary := false
		for _, msg := range msgs {
			for _, c := range msg.Content {
				if strings.Contains(c.Text, "Previous conversation summary") {
					hasPreviousSummary = true
				}
			}
		}
		if hasPreviousSummary {
			return "DECISIONS: Decided to use Go. Added caching layer.\nFACTS: Project uses SQLite. Cache TTL is 5 minutes.\nACTIONS: Setup and caching complete.\nCONTEXT: User prefers concise output.", nil
		}
		return "DECISIONS: Decided to use Go.\nFACTS: Project uses SQLite.\nACTIONS: Setup complete.\nCONTEXT: User prefers concise output.", nil
	}
	cm := NewCompactionManager(mem, llm)

	// --- First compaction ---
	var msgs1 []canonical.Message
	for i := 0; i < 60; i++ {
		msgs1 = append(msgs1,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "discuss Go setup"}}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Go is configured"}}},
		)
	}
	result1 := cm.TryCompact(context.Background(), "test-iterative-1", msgs1, DefaultCompactionConfig(), 200000)
	if result1 == nil {
		t.Fatal("first compaction should produce results")
	}

	// First message should be the summary.
	if !strings.Contains(result1[0].Content[0].Text, "Previous conversation summary") {
		t.Fatal("first message should be the injected summary")
	}
	if !strings.Contains(result1[0].Content[0].Text, "Decided to use Go") {
		t.Fatal("summary should contain first-round decisions")
	}

	// --- Second compaction: summary + new messages ---
	var msgs2 []canonical.Message
	msgs2 = append(msgs2, result1...) // Includes the summary as first message.
	for i := 0; i < 60; i++ {
		msgs2 = append(msgs2,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "add caching layer"}}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "caching configured with 5min TTL"}}},
		)
	}

	// Reset tracking for second compaction.
	llmCalls = nil
	llmPrompts = nil
	result2 := cm.TryCompact(context.Background(), "test-iterative-2", msgs2, DefaultCompactionConfig(), 200000)
	if result2 == nil {
		t.Fatal("second compaction should produce results")
	}

	// Second compaction makes 2 LLM calls: flush + summarize.
	if len(llmCalls) < 2 {
		t.Fatalf("expected at least 2 LLM calls (flush + summarize), got %d", len(llmCalls))
	}

	// Find the summarization call (not the flush call).
	var summarizeInput []canonical.Message
	for i, prompt := range llmPrompts {
		if strings.Contains(prompt, "Summarize") {
			summarizeInput = llmCalls[i]
			break
		}
	}
	if summarizeInput == nil {
		t.Fatal("could not find the summarization LLM call")
	}

	// The key assertion: the summarizer receives the previous summary.
	foundPreviousSummary := false
	for _, msg := range summarizeInput {
		for _, c := range msg.Content {
			if strings.Contains(c.Text, "Previous conversation summary") {
				foundPreviousSummary = true
			}
		}
	}
	if !foundPreviousSummary {
		t.Error("second summarization should receive the previous summary in its input messages")
	}

	// Final summary should contain merged content from both rounds.
	finalSummary := result2[0].Content[0].Text
	if !strings.Contains(finalSummary, "caching") {
		t.Error("final summary should contain new content (caching)")
	}
}

// TestIterativeCompaction_Integration tests three compaction cycles with a real
// LLM to verify no information loss across iterations. Requires SAGECLAW_TEST_API_KEY.
func TestIterativeCompaction_Integration(t *testing.T) {
	apiKey := os.Getenv("SAGECLAW_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("SAGECLAW_TEST_API_KEY not set — skipping integration test")
	}
	// This test would wire a real cheap model provider and run three
	// compaction cycles with distinct facts in each, verifying the final
	// summary retains information from all three. Placeholder for now —
	// full implementation requires provider wiring outside unit test scope.
	t.Skip("integration test placeholder — requires provider wiring")
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
