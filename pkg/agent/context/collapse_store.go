package context

import (
	"sync"
	"time"
)

// CollapseEntry represents a collapsed range of messages, identified by
// iteration numbers. Iteration-based ranges are stable across history
// mutations (unlike array indices which shift when messages are added
// or removed by auto-compact).
type CollapseEntry struct {
	StartIter  int          // First iteration in the collapsed range (for display).
	EndIter    int          // Last iteration in the collapsed range (for display).
	Iterations map[int]bool // Exact set of collapsed iterations (for matching).
	Summary    string       // LLM-generated summary of the range.
	CreatedAt  time.Time    // When this collapse was created.
	Tokens     int          // Estimated tokens saved by this collapse.
}

// CollapseStore holds summaries for collapsed message ranges, keyed by session.
type CollapseStore struct {
	mu        sync.RWMutex
	collapses map[string][]CollapseEntry // sessionID -> entries
}

// NewCollapseStore creates a new empty collapse store.
func NewCollapseStore() *CollapseStore {
	return &CollapseStore{
		collapses: make(map[string][]CollapseEntry),
	}
}

// Add stores a new collapse entry for a session.
func (cs *CollapseStore) Add(sessionID string, entry CollapseEntry) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.collapses[sessionID] = append(cs.collapses[sessionID], entry)
}

// Get returns all collapse entries for a session (may be empty).
func (cs *CollapseStore) Get(sessionID string) []CollapseEntry {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	entries := cs.collapses[sessionID]
	// Return a copy to prevent mutation.
	if len(entries) == 0 {
		return nil
	}
	cp := make([]CollapseEntry, len(entries))
	copy(cp, entries)
	return cp
}

// Invalidate clears all collapse entries for a session. Called after
// auto-compact destroys the messages these entries reference.
func (cs *CollapseStore) Invalidate(sessionID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.collapses, sessionID)
}

// HasCollapses returns true if the session has any collapse entries.
func (cs *CollapseStore) HasCollapses(sessionID string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.collapses[sessionID]) > 0
}
