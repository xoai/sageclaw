package middleware

import (
	"context"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// mockEngine returns canned search results.
type mockEngine struct {
	results []memory.Entry
}

func (m *mockEngine) Write(_ context.Context, _, _ string, _ []string) (string, error) {
	return "id", nil
}
func (m *mockEngine) Search(_ context.Context, _ string, opts memory.SearchOptions) ([]memory.Entry, error) {
	var filtered []memory.Entry
	for _, e := range m.results {
		// Filter by MinConfidence: entries with "low-conf" in title have confidence 0.2.
		if opts.MinConfidence > 0 && strings.Contains(e.Title, "low-conf") {
			continue
		}
		// Filter by FilterTags: all filter tags must be present on the entry.
		if len(opts.FilterTags) > 0 {
			allMatch := true
			for _, ft := range opts.FilterTags {
				found := false
				for _, et := range e.Tags {
					if et == ft {
						found = true
						break
					}
				}
				if !found {
					allMatch = false
					break
				}
			}
			if !allMatch {
				continue
			}
		}
		filtered = append(filtered, e)
	}
	return filtered, nil
}
func (m *mockEngine) List(_ context.Context, _ []string, _, _ int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockEngine) Delete(_ context.Context, _ string) error { return nil }

func runPreContext(mw Middleware, msgs []canonical.Message) (*HookData, error) {
	data := &HookData{
		HookPoint: HookPreContext,
		Messages:  msgs,
	}
	err := mw(context.Background(), data, func(_ context.Context, _ *HookData) error { return nil })
	return data, err
}

func userMsg(text string) []canonical.Message {
	return []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: text}}},
	}
}

func TestPreContextSelfLearning_SeparatesProcedures(t *testing.T) {
	engine := &mockEngine{
		results: []memory.Entry{
			{Title: "correction-1", Content: "Don't use fmt.Println in production", Tags: []string{"self-learning", "correction"}},
			{Title: "[PROC] Setup channels", Content: "TRIGGER: new channel\nSTEPS:\n1. Create adapter", Tags: []string{"self-learning", "procedure"}},
			{Title: "gotcha-1", Content: "SQLite needs WAL mode for concurrency", Tags: []string{"self-learning", "gotcha"}},
		},
	}

	mw := PreContextSelfLearning(engine)
	data, err := runPreContext(mw, userMsg("how to add a channel"))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Injections) != 2 {
		t.Fatalf("expected 2 injections (warnings + procedures), got %d", len(data.Injections))
	}

	// First injection should be warnings.
	if !strings.Contains(data.Injections[0], "Warnings from past experience") {
		t.Error("first injection should be warnings")
	}
	if strings.Contains(data.Injections[0], "[PROC]") {
		t.Error("warnings should NOT contain procedures")
	}

	// Second injection should be procedures.
	if !strings.Contains(data.Injections[1], "Relevant procedures from past experience") {
		t.Error("second injection should be procedures")
	}
	if !strings.Contains(data.Injections[1], "TRIGGER") {
		t.Error("procedure injection should include the procedure content")
	}
}

func TestPreContextSelfLearning_CorrectionsOnly(t *testing.T) {
	engine := &mockEngine{
		results: []memory.Entry{
			{Title: "correction-1", Content: "Always use context.WithTimeout", Tags: []string{"self-learning", "correction"}},
		},
	}

	mw := PreContextSelfLearning(engine)
	data, err := runPreContext(mw, userMsg("add a timeout"))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Injections) != 1 {
		t.Fatalf("expected 1 injection (warnings only), got %d", len(data.Injections))
	}
	if !strings.Contains(data.Injections[0], "Warnings") {
		t.Error("should be warnings injection")
	}
}

func TestPreContextSelfLearning_ProceduresOnly(t *testing.T) {
	engine := &mockEngine{
		results: []memory.Entry{
			{Title: "[PROC] Debug races", Content: "TRIGGER: flaky test\nSTEPS: 1. go test -race", Tags: []string{"self-learning", "procedure"}},
		},
	}

	mw := PreContextSelfLearning(engine)
	data, err := runPreContext(mw, userMsg("tests are flaky"))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Injections) != 1 {
		t.Fatalf("expected 1 injection (procedures only), got %d", len(data.Injections))
	}
	if !strings.Contains(data.Injections[0], "Relevant procedures") {
		t.Error("should be procedures injection")
	}
}

func TestPreContextSelfLearning_EmptyResults(t *testing.T) {
	engine := &mockEngine{results: nil}

	mw := PreContextSelfLearning(engine)
	data, err := runPreContext(mw, userMsg("hello"))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Injections) != 0 {
		t.Errorf("empty results should produce no injections, got %d", len(data.Injections))
	}
}

func TestPreContextSelfLearning_ExistingCorrectionsStillSurface(t *testing.T) {
	// Existing corrections have default confidence 0.8 — they should pass
	// the MinConfidence: 0.3 threshold set in the updated middleware.
	engine := &mockEngine{
		results: []memory.Entry{
			{Title: "old-correction", Content: "Use structured logging", Tags: []string{"self-learning", "convention"}},
		},
	}

	mw := PreContextSelfLearning(engine)
	data, err := runPreContext(mw, userMsg("add logging"))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Injections) != 1 {
		t.Fatalf("existing corrections (confidence 0.8) should surface, got %d injections", len(data.Injections))
	}
}

func TestPreContextSelfLearning_SkipsNonPreContext(t *testing.T) {
	engine := &mockEngine{
		results: []memory.Entry{
			{Title: "test", Content: "content", Tags: []string{"self-learning"}},
		},
	}

	mw := PreContextSelfLearning(engine)
	data := &HookData{
		HookPoint: HookPostTool, // Not PreContext.
		Messages:  userMsg("hello"),
	}
	err := mw(context.Background(), data, func(_ context.Context, _ *HookData) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Injections) != 0 {
		t.Error("should not inject on non-PreContext hook")
	}
}

func TestPreContextMemory_InjectsContext(t *testing.T) {
	engine := &mockEngine{
		results: []memory.Entry{
			{Title: "project-info", Content: "SageClaw uses Go and SQLite", Tags: []string{"context"}},
		},
	}

	mw := PreContextMemory(engine)
	data, err := runPreContext(mw, userMsg("what stack do we use"))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Injections) != 1 {
		t.Fatalf("expected 1 injection, got %d", len(data.Injections))
	}
	if !strings.Contains(data.Injections[0], "Relevant context from memory") {
		t.Error("should inject memory context")
	}
}

func TestHasTag(t *testing.T) {
	if !hasTag([]string{"a", "procedure", "b"}, "procedure") {
		t.Error("should find 'procedure' tag")
	}
	if hasTag([]string{"a", "b"}, "procedure") {
		t.Error("should not find missing tag")
	}
	if hasTag(nil, "anything") {
		t.Error("nil tags should return false")
	}
}
