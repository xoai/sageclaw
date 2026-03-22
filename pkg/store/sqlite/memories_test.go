package sqlite

import (
	"context"
	"testing"
)

func TestMemory_WriteAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, dup, err := s.WriteMemory(ctx, "Go uses goroutines for concurrency", "Go concurrency", []string{"go", "concurrency"})
	if err != nil {
		t.Fatalf("writing memory: %v", err)
	}
	if dup {
		t.Fatal("expected non-duplicate")
	}
	if id == "" {
		t.Fatal("expected ID")
	}

	m, err := s.GetMemory(ctx, id)
	if err != nil {
		t.Fatalf("getting memory: %v", err)
	}
	if m.Content != "Go uses goroutines for concurrency" {
		t.Fatalf("unexpected content: %s", m.Content)
	}
	if m.Title != "Go concurrency" {
		t.Fatalf("unexpected title: %s", m.Title)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "go" {
		t.Fatalf("unexpected tags: %v", m.Tags)
	}
}

func TestMemory_AutoTitle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _, err := s.WriteMemory(ctx, "Short content here", "", nil)
	if err != nil {
		t.Fatalf("writing memory: %v", err)
	}

	m, err := s.GetMemory(ctx, id)
	if err != nil {
		t.Fatalf("getting memory: %v", err)
	}
	if m.Title != "Short content here" {
		t.Fatalf("expected auto-title, got: %s", m.Title)
	}
}

func TestMemory_Dedup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id1, _, err := s.WriteMemory(ctx, "duplicate content", "first", nil)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	id2, dup, err := s.WriteMemory(ctx, "duplicate content", "second", nil)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if !dup {
		t.Fatal("expected duplicate detection")
	}
	if id2 != id1 {
		t.Fatalf("expected same ID %s, got %s", id1, id2)
	}
}

func TestMemory_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _, _ := s.WriteMemory(ctx, "to be deleted", "", nil)
	if err := s.DeleteMemory(ctx, id); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	_, err := s.GetMemory(ctx, id)
	if err == nil {
		t.Fatal("expected error getting deleted memory")
	}
}

func TestMemory_List(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.WriteMemory(ctx, "memory about go", "go mem", []string{"go"})
	s.WriteMemory(ctx, "memory about rust", "rust mem", []string{"rust"})
	s.WriteMemory(ctx, "memory about go and rust", "both", []string{"go", "rust"})

	// List all.
	all, err := s.ListMemories(ctx, nil, 10, 0)
	if err != nil {
		t.Fatalf("listing all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(all))
	}

	// Filter by tag "go".
	goMems, err := s.ListMemories(ctx, []string{"go"}, 10, 0)
	if err != nil {
		t.Fatalf("listing go: %v", err)
	}
	if len(goMems) != 2 {
		t.Fatalf("expected 2 go memories, got %d", len(goMems))
	}

	// Filter by both tags (AND logic).
	bothMems, err := s.ListMemories(ctx, []string{"go", "rust"}, 10, 0)
	if err != nil {
		t.Fatalf("listing both: %v", err)
	}
	if len(bothMems) != 1 {
		t.Fatalf("expected 1 memory with both tags, got %d", len(bothMems))
	}
}

func TestMemory_SearchFTS5(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.WriteMemory(ctx, "Go channels enable safe communication between goroutines", "Go channels", []string{"go"})
	s.WriteMemory(ctx, "Python uses the GIL for thread safety", "Python GIL", []string{"python"})
	s.WriteMemory(ctx, "Rust ownership model prevents data races at compile time", "Rust ownership", []string{"rust"})

	// Search for "goroutines".
	results, scores, err := s.SearchMemories(ctx, "goroutines", 10)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Title != "Go channels" {
		t.Fatalf("expected Go channels as top result, got: %s", results[0].Title)
	}
	if len(scores) != len(results) {
		t.Fatal("scores length mismatch")
	}

	// Search for "thread safety".
	results2, _, err := s.SearchMemories(ctx, "thread safety", 10)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results2) == 0 {
		t.Fatal("expected results for 'thread safety'")
	}
}

func TestMemory_SearchFTS5_NoResults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.WriteMemory(ctx, "something about databases", "", nil)

	results, _, err := s.SearchMemories(ctx, "quantum physics", 10)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func TestMemory_AccessTracking(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _, _ := s.WriteMemory(ctx, "track my access", "", nil)

	// Get it twice.
	s.GetMemory(ctx, id)
	m, _ := s.GetMemory(ctx, id)
	if m.AccessCount < 1 {
		t.Fatalf("expected access_count >= 1, got %d", m.AccessCount)
	}
}
