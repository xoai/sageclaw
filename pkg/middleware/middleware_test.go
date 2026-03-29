package middleware

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/memory/fts5"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestEngine(t *testing.T) memory.MemoryEngine {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return fts5.New(store)
}

func TestChain_ExecutionOrder(t *testing.T) {
	var order []string

	mw1 := func(ctx context.Context, data *HookData, next NextFunc) error {
		order = append(order, "mw1-before")
		err := next(ctx, data)
		order = append(order, "mw1-after")
		return err
	}

	mw2 := func(ctx context.Context, data *HookData, next NextFunc) error {
		order = append(order, "mw2-before")
		err := next(ctx, data)
		order = append(order, "mw2-after")
		return err
	}

	chain := Chain(mw1, mw2)

	data := &HookData{}
	chain(context.Background(), data, func(ctx context.Context, data *HookData) error {
		order = append(order, "handler")
		return nil
	})

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d steps, got %d: %v", len(expected), len(order), order)
	}
	for i, step := range expected {
		if order[i] != step {
			t.Fatalf("step %d: expected %s, got %s", i, step, order[i])
		}
	}
}

func TestPreContextMemory_InjectsRelevantContext(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// Seed a memory.
	engine.Write(ctx, "The deploy script is at scripts/deploy.sh and requires AWS credentials", "Deploy script location", []string{"devops"})

	mw := PreContextMemory(engine)
	data := &HookData{
		HookPoint: HookPreContext,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "how do I deploy this project"}}},
		},
	}

	err := mw(ctx, data, func(ctx context.Context, data *HookData) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if len(data.Injections) == 0 {
		t.Fatal("expected injections from memory search")
	}
	if !strings.Contains(data.Injections[0], "deploy") {
		t.Fatalf("expected deploy context, got: %s", data.Injections[0])
	}
}

func TestPreContextSelfLearning_InjectsWarnings(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// Seed a self-learning rule.
	engine.Write(ctx, "When running database migrations, always backup first. Previous migration caused data loss.", "Backup before migrations", []string{"self-learning", "database"})

	mw := PreContextSelfLearning(engine)
	data := &HookData{
		HookPoint: HookPreContext,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "run the database migration"}}},
		},
	}

	err := mw(ctx, data, func(ctx context.Context, data *HookData) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if len(data.Injections) == 0 {
		t.Fatal("expected self-learning warning injection")
	}
	if !strings.Contains(data.Injections[0], "migration") {
		t.Fatalf("expected migration warning, got: %s", data.Injections[0])
	}
}

func TestPostToolScrub_RedactsSecrets(t *testing.T) {
	mw := PostToolScrub()
	data := &HookData{
		HookPoint: HookPostTool,
		ToolCall:  &canonical.ToolCall{Name: "read_file"},
		ToolResult: &canonical.ToolResult{
			Content: "Config: api_key=sk-ant-secret-key-12345678901234567890",
		},
	}

	err := mw(context.Background(), data, func(ctx context.Context, data *HookData) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if strings.Contains(data.ToolResult.Content, "sk-ant") {
		t.Fatalf("expected secret to be scrubbed, got: %s", data.ToolResult.Content)
	}
	if !strings.Contains(data.ToolResult.Content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED], got: %s", data.ToolResult.Content)
	}
}

func TestPostToolAudit_LogsEntry(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	mw := PostToolAudit(store)
	data := &HookData{
		HookPoint: HookPostTool,
		ToolCall:  &canonical.ToolCall{Name: "read_file", Input: []byte(`{"path":"test.txt"}`)},
		ToolResult: &canonical.ToolResult{Content: "file contents"},
		Metadata:   map[string]any{"session_id": "sess_1", "agent_id": "agent_1"},
	}

	err = mw(context.Background(), data, func(ctx context.Context, data *HookData) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	// Verify audit log entry.
	var count int
	store.DB().QueryRow("SELECT COUNT(*) FROM audit_log WHERE session_id = 'sess_1'").Scan(&count)
	if count == 0 {
		t.Fatal("expected audit log entry")
	}
}

func TestPreContextMemory_ConfidenceFilter(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// Write a high-confidence memory.
	engine.Write(ctx, "High confidence: the API key format is sk-xxx", "API key format", []string{"devops"})

	// Write a low-confidence memory via WriteWithConfidence.
	if cw, ok := engine.(memory.ConfidenceWriter); ok {
		cw.WriteWithConfidence(ctx, "Low confidence: user might prefer dark mode", "UI preference", []string{"preference"}, 0.3)
	}

	// Use MinConfidence 0.5 — should only return the high-confidence one.
	mw := PreContextMemoryWithConfig(engine, 0.5, DefaultMaxInjectionTokens)
	data := &HookData{
		HookPoint: HookPreContext,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "API key dark mode"}}},
		},
	}

	err := mw(ctx, data, func(ctx context.Context, data *HookData) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if len(data.Injections) == 0 {
		t.Fatal("expected at least one injection")
	}
	injection := data.Injections[0]
	if !strings.Contains(injection, "API key") {
		t.Fatalf("expected high-confidence memory, got: %s", injection)
	}
	if strings.Contains(injection, "dark mode") {
		t.Fatalf("low-confidence memory should be filtered out, got: %s", injection)
	}
}

func TestPreContextMemory_InjectionTokenCap(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// Seed many unique memories with substantial content.
	for i := 0; i < 10; i++ {
		content := fmt.Sprintf("Server configuration item %d with many important details about server setup and infrastructure management for production environments", i)
		engine.Write(ctx, content, fmt.Sprintf("Server config %d", i), []string{"infra"})
	}

	// Verify memories were written.
	results, err := engine.Search(ctx, "server configuration", memory.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no memories found after seeding — test setup issue")
	}

	// Use a very small token cap (50 tokens ≈ 200 chars).
	mw := PreContextMemoryWithConfig(engine, 0.0, 50)
	data := &HookData{
		HookPoint: HookPreContext,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "server configuration setup"}}},
		},
	}

	err = mw(ctx, data, func(ctx context.Context, data *HookData) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}

	if len(data.Injections) == 0 {
		t.Fatal("expected injections")
	}

	// Count how many "Server config" lines appear. With 50 token cap,
	// should not include all 10 memories.
	lineCount := strings.Count(data.Injections[0], "Server config")
	if lineCount >= 10 {
		t.Errorf("expected cap to limit results, but got all %d memories", lineCount)
	}
}

func TestPreContext_SkipsOnWrongHookPoint(t *testing.T) {
	engine := newTestEngine(t)
	mw := PreContextMemory(engine)

	data := &HookData{HookPoint: HookPostTool}
	called := false
	mw(context.Background(), data, func(ctx context.Context, data *HookData) error {
		called = true
		return nil
	})

	if !called {
		t.Fatal("expected next to be called for wrong hook point")
	}
}
