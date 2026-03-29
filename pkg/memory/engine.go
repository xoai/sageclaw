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
	Tags          []string // Soft boost — results with these tags rank higher
	FilterTags    []string // Hard AND filter — only return memories matching ALL
	Limit         int      // Max results (default 10)
	MinConfidence float64  // Minimum confidence threshold (0 = no filter, default)
}

// MemoryEngine is the knowledge boundary interface (ADR-002 exception).
// Implementations: fts5.Engine (default), future vector/hybrid backends.
type MemoryEngine interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]Entry, error)
	Write(ctx context.Context, content string, title string, tags []string) (string, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, tags []string, limit, offset int) ([]Entry, error)
}

// ConfidenceWriter is an optional interface for memory engines that support
// confidence-weighted writes. Self-learning corrections use 0.9, general
// facts 0.7, inferred preferences 0.5.
type ConfidenceWriter interface {
	WriteWithConfidence(ctx context.Context, content, title string, tags []string, confidence float64) (string, error)
}
