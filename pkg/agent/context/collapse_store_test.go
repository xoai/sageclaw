package context

import (
	"sync"
	"testing"
	"time"
)

func TestCollapseStore_AddAndGet(t *testing.T) {
	cs := NewCollapseStore()
	entry := CollapseEntry{
		StartIter: 1,
		EndIter:   5,
		Summary:   "Read files and analyzed code",
		CreatedAt: time.Now(),
		Tokens:    500,
	}

	cs.Add("sess1", entry)
	entries := cs.Get("sess1")

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].StartIter != 1 || entries[0].EndIter != 5 {
		t.Error("wrong iteration range")
	}
	if entries[0].Summary != "Read files and analyzed code" {
		t.Error("wrong summary")
	}
}

func TestCollapseStore_GetEmpty(t *testing.T) {
	cs := NewCollapseStore()
	entries := cs.Get("nosuch")

	if entries != nil {
		t.Errorf("expected nil for empty session, got %v", entries)
	}
}

func TestCollapseStore_GetReturnsCopy(t *testing.T) {
	cs := NewCollapseStore()
	cs.Add("sess1", CollapseEntry{StartIter: 1, EndIter: 3, Summary: "a"})

	entries := cs.Get("sess1")
	entries[0].Summary = "mutated"

	// Original should be unchanged.
	original := cs.Get("sess1")
	if original[0].Summary != "a" {
		t.Error("Get should return a copy, not a reference")
	}
}

func TestCollapseStore_Invalidate(t *testing.T) {
	cs := NewCollapseStore()
	cs.Add("sess1", CollapseEntry{StartIter: 1, EndIter: 5, Summary: "a"})
	cs.Add("sess1", CollapseEntry{StartIter: 6, EndIter: 10, Summary: "b"})

	cs.Invalidate("sess1")

	entries := cs.Get("sess1")
	if entries != nil {
		t.Errorf("expected nil after invalidation, got %d entries", len(entries))
	}
}

func TestCollapseStore_InvalidateNonexistent(t *testing.T) {
	cs := NewCollapseStore()
	// Should not panic.
	cs.Invalidate("nosuch")
}

func TestCollapseStore_MultipleEntries(t *testing.T) {
	cs := NewCollapseStore()
	cs.Add("sess1", CollapseEntry{StartIter: 1, EndIter: 5, Summary: "first"})
	cs.Add("sess1", CollapseEntry{StartIter: 6, EndIter: 10, Summary: "second"})

	entries := cs.Get("sess1")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestCollapseStore_SessionIsolation(t *testing.T) {
	cs := NewCollapseStore()
	cs.Add("sess1", CollapseEntry{StartIter: 1, EndIter: 5, Summary: "s1"})
	cs.Add("sess2", CollapseEntry{StartIter: 1, EndIter: 3, Summary: "s2"})

	cs.Invalidate("sess1")

	if cs.Get("sess1") != nil {
		t.Error("sess1 should be empty after invalidation")
	}
	if len(cs.Get("sess2")) != 1 {
		t.Error("sess2 should be unaffected by sess1 invalidation")
	}
}

func TestCollapseStore_HasCollapses(t *testing.T) {
	cs := NewCollapseStore()

	if cs.HasCollapses("sess1") {
		t.Error("empty store should not have collapses")
	}

	cs.Add("sess1", CollapseEntry{StartIter: 1, EndIter: 5, Summary: "a"})
	if !cs.HasCollapses("sess1") {
		t.Error("store with entries should have collapses")
	}
}

func TestCollapseStore_ConcurrentAccess(t *testing.T) {
	cs := NewCollapseStore()
	var wg sync.WaitGroup

	// Concurrent writes.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cs.Add("sess1", CollapseEntry{StartIter: n, EndIter: n + 1, Summary: "test"})
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cs.Get("sess1")
		}()
	}

	wg.Wait()

	entries := cs.Get("sess1")
	if len(entries) != 20 {
		t.Errorf("expected 20 entries after concurrent writes, got %d", len(entries))
	}
}
