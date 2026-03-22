package fts5

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return New(store)
}

func seedMemories(t *testing.T, e *Engine) {
	t.Helper()
	ctx := context.Background()
	memories := []struct {
		content string
		title   string
		tags    []string
	}{
		{"Go channels enable safe communication between goroutines", "Go channels", []string{"go", "concurrency"}},
		{"Python uses the GIL for thread safety but it limits parallelism", "Python GIL", []string{"python", "concurrency"}},
		{"Rust ownership model prevents data races at compile time", "Rust ownership", []string{"rust", "safety"}},
		{"SQLite FTS5 provides full-text search with BM25 ranking", "SQLite FTS5", []string{"sqlite", "search"}},
		{"Kubernetes orchestrates containers across a cluster of machines", "Kubernetes", []string{"devops", "containers"}},
		{"React uses a virtual DOM to minimize browser reflows", "React VDOM", []string{"frontend", "javascript"}},
		{"PostgreSQL supports JSONB for document-style queries", "PostgreSQL JSONB", []string{"database", "sql"}},
		{"Docker containers share the host kernel for lightweight isolation", "Docker", []string{"devops", "containers"}},
		{"gRPC uses Protocol Buffers for efficient binary serialization", "gRPC", []string{"networking", "api"}},
		{"Redis provides in-memory data structures for caching", "Redis caching", []string{"database", "caching"}},
	}
	for _, m := range memories {
		if _, err := e.Write(ctx, m.content, m.title, m.tags); err != nil {
			t.Fatalf("seeding memory: %v", err)
		}
	}
}

func TestEngine_WriteAndSearch(t *testing.T) {
	e := newTestEngine(t)
	seedMemories(t, e)
	ctx := context.Background()

	results, err := e.Search(ctx, "goroutines concurrency", memory.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'goroutines concurrency'")
	}
	// Go channels should be top result — it has both terms.
	if results[0].Title != "Go channels" {
		t.Fatalf("expected Go channels as top result, got: %s", results[0].Title)
	}
	if results[0].Score <= 0 {
		t.Fatalf("expected positive score, got: %f", results[0].Score)
	}
}

func TestEngine_SearchWithTagBoost(t *testing.T) {
	e := newTestEngine(t)
	seedMemories(t, e)
	ctx := context.Background()

	// Search for "containers" with devops tag boost.
	results, err := e.Search(ctx, "containers", memory.SearchOptions{
		Tags:  []string{"devops"},
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// Both Kubernetes and Docker should be found; tag boost should favor devops-tagged ones.
	for _, r := range results {
		if r.Score <= 0 {
			t.Fatalf("expected positive score for %s, got: %f", r.Title, r.Score)
		}
	}
}

func TestEngine_SearchWithFilterTags(t *testing.T) {
	e := newTestEngine(t)
	seedMemories(t, e)
	ctx := context.Background()

	// Search broadly but filter to only "database" tag.
	results, err := e.Search(ctx, "data structures queries caching", memory.SearchOptions{
		FilterTags: []string{"database"},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	// All results should have "database" tag.
	for _, r := range results {
		hasDB := false
		for _, tag := range r.Tags {
			if tag == "database" {
				hasDB = true
				break
			}
		}
		if !hasDB {
			t.Fatalf("result %s should have database tag, has: %v", r.Title, r.Tags)
		}
	}
}

func TestEngine_SearchEmpty(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	_, err := e.Search(ctx, "", memory.SearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestEngine_SearchNoResults(t *testing.T) {
	e := newTestEngine(t)
	seedMemories(t, e)
	ctx := context.Background()

	results, err := e.Search(ctx, "quantum entanglement teleportation", memory.SearchOptions{})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func TestEngine_WriteDedup(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	id1, err := e.Write(ctx, "same content twice", "first", nil)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	id2, err := e.Write(ctx, "same content twice", "second", nil)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same ID for dedup, got %s and %s", id1, id2)
	}
}

func TestEngine_Delete(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	id, _ := e.Write(ctx, "to be deleted from memory", "delete me", nil)
	if err := e.Delete(ctx, id); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	// Should not appear in search.
	results, _ := e.Search(ctx, "deleted memory", memory.SearchOptions{})
	for _, r := range results {
		if r.ID == id {
			t.Fatal("deleted memory should not appear in search")
		}
	}
}

func TestEngine_List(t *testing.T) {
	e := newTestEngine(t)
	seedMemories(t, e)
	ctx := context.Background()

	all, err := e.List(ctx, nil, 100, 0)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(all) != 10 {
		t.Fatalf("expected 10 memories, got %d", len(all))
	}

	// Filter by tag.
	devops, err := e.List(ctx, []string{"devops"}, 100, 0)
	if err != nil {
		t.Fatalf("listing devops: %v", err)
	}
	if len(devops) != 2 {
		t.Fatalf("expected 2 devops memories, got %d", len(devops))
	}
}

func TestEngine_ListPagination(t *testing.T) {
	e := newTestEngine(t)
	seedMemories(t, e)
	ctx := context.Background()

	page1, err := e.List(ctx, nil, 3, 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("expected 3, got %d", len(page1))
	}

	page2, err := e.List(ctx, nil, 3, 3)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 3 {
		t.Fatalf("expected 3, got %d", len(page2))
	}

	// Pages should have different entries.
	if page1[0].ID == page2[0].ID {
		t.Fatal("page 1 and 2 should have different entries")
	}
}

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"goroutines concurrency", `"goroutines" OR "concurrency"`},
		{"the a is", ""},                        // All stop words.
		{"Go", `"go"`},                          // "go" is 2 chars, valid term.
		{"sqlite fts5", `"sqlite" OR "fts5"`},
		{"", ""},
	}
	for _, tt := range tests {
		got := buildFTSQuery(tt.input)
		if got != tt.expected {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
