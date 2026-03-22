package memory

import (
	"context"
	"time"
)

// Entry represents a memory search result or stored memory.
type Entry struct {
	ID        string
	Title     string
	Content   string
	Tags      []string
	CreatedAt time.Time
	UpdatedAt time.Time
	Score     float64 // BM25 relevance score (search results only)
}

// SearchOptions controls memory search behavior.
type SearchOptions struct {
	Tags       []string // Soft boost — results with these tags rank higher
	FilterTags []string // Hard AND filter — only return memories matching ALL
	Limit      int      // Max results (default 10)
}

// MemoryEngine is the knowledge boundary interface (ADR-002 exception).
// Implementations: fts5.Engine (default), future vector/hybrid backends.
type MemoryEngine interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]Entry, error)
	Write(ctx context.Context, content string, title string, tags []string) (string, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, tags []string, limit, offset int) ([]Entry, error)
}
