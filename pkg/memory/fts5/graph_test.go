package fts5

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestGraphOps(t *testing.T) (*GraphOps, *Engine) {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewGraphOps(store), New(store)
}

func TestGraph_LinkAndUnlink(t *testing.T) {
	g, e := newTestGraphOps(t)
	ctx := context.Background()

	id1, _ := e.Write(ctx, "Go programming language", "Go", []string{"lang"})
	id2, _ := e.Write(ctx, "Goroutines for concurrency", "Goroutines", []string{"concurrency"})

	edgeID, err := g.Link(ctx, id1, id2, "has_feature", nil)
	if err != nil {
		t.Fatalf("linking: %v", err)
	}
	if edgeID == "" {
		t.Fatal("expected edge ID")
	}

	// Unlink.
	if err := g.Unlink(ctx, id1, id2, "has_feature"); err != nil {
		t.Fatalf("unlinking: %v", err)
	}

	// Should fail to unlink again.
	if err := g.Unlink(ctx, id1, id2, "has_feature"); err == nil {
		t.Fatal("expected error unlinking nonexistent edge")
	}
}

func TestGraph_SelfLink(t *testing.T) {
	g, e := newTestGraphOps(t)
	ctx := context.Background()

	id1, _ := e.Write(ctx, "Node", "Node", nil)
	_, err := g.Link(ctx, id1, id1, "self", nil)
	if err == nil {
		t.Fatal("expected error for self-link")
	}
}

func TestGraph_Traversal(t *testing.T) {
	g, e := newTestGraphOps(t)
	ctx := context.Background()

	// Create a chain: A → B → C.
	idA, _ := e.Write(ctx, "Node A", "A", nil)
	idB, _ := e.Write(ctx, "Node B", "B", nil)
	idC, _ := e.Write(ctx, "Node C", "C", nil)

	g.Link(ctx, idA, idB, "connects", nil)
	g.Link(ctx, idB, idC, "connects", nil)

	// Traverse outbound from A, depth 2.
	entries, edges, err := g.Graph(ctx, idA, "outbound", 2)
	if err != nil {
		t.Fatalf("traversing: %v", err)
	}

	// Should find A, B, C.
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edges))
	}
}

func TestGraph_TraversalDepthLimit(t *testing.T) {
	g, e := newTestGraphOps(t)
	ctx := context.Background()

	idA, _ := e.Write(ctx, "Node A", "A", nil)
	idB, _ := e.Write(ctx, "Node B", "B", nil)
	idC, _ := e.Write(ctx, "Node C", "C", nil)

	g.Link(ctx, idA, idB, "connects", nil)
	g.Link(ctx, idB, idC, "connects", nil)

	// Depth 1: should find A and B only.
	entries, _, err := g.Graph(ctx, idA, "outbound", 1)
	if err != nil {
		t.Fatalf("traversing: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries at depth 1, got %d", len(entries))
	}
}

func TestGraph_CycleDetection(t *testing.T) {
	g, e := newTestGraphOps(t)
	ctx := context.Background()

	// Create a cycle: A → B → C → A.
	idA, _ := e.Write(ctx, "Node A", "A", nil)
	idB, _ := e.Write(ctx, "Node B", "B", nil)
	idC, _ := e.Write(ctx, "Node C", "C", nil)

	g.Link(ctx, idA, idB, "connects", nil)
	g.Link(ctx, idB, idC, "connects", nil)
	g.Link(ctx, idC, idA, "connects", nil)

	// Should not infinite-loop.
	entries, edges, err := g.Graph(ctx, idA, "outbound", 5)
	if err != nil {
		t.Fatalf("traversing: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (cycle), got %d", len(entries))
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges (cycle), got %d", len(edges))
	}
}
