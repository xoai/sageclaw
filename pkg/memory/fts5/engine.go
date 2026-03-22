package fts5

import (
	"context"
	"fmt"

	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

// Engine implements memory.MemoryEngine using FTS5 BM25 search.
type Engine struct {
	store *sqlite.Store
}

// New creates a new FTS5 memory engine backed by the given store.
func New(store *sqlite.Store) *Engine {
	return &Engine{store: store}
}

// Write stores a new memory and returns its ID.
func (e *Engine) Write(ctx context.Context, content, title string, tags []string) (string, error) {
	id, dup, err := e.store.WriteMemory(ctx, content, title, tags)
	if err != nil {
		return "", err
	}
	if dup {
		return id, nil // Return existing ID for duplicates.
	}
	return id, nil
}

// Delete removes a memory by ID.
func (e *Engine) Delete(ctx context.Context, id string) error {
	return e.store.DeleteMemory(ctx, id)
}

// List returns memories optionally filtered by tags.
func (e *Engine) List(ctx context.Context, tags []string, limit, offset int) ([]memory.Entry, error) {
	mems, err := e.store.ListMemories(ctx, tags, limit, offset)
	if err != nil {
		return nil, err
	}
	entries := make([]memory.Entry, len(mems))
	for i, m := range mems {
		entries[i] = toEntry(m)
	}
	return entries, nil
}

// Search performs a FTS5 BM25 search with tag boosting and recency decay.
func (e *Engine) Search(ctx context.Context, query string, opts memory.SearchOptions) ([]memory.Entry, error) {
	if query == "" {
		return nil, fmt.Errorf("search query must not be empty")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	// Build the FTS5 query from the search terms.
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	// Fetch more than needed to allow for filtering and re-ranking.
	fetchLimit := limit * 3
	if fetchLimit < 30 {
		fetchLimit = 30
	}

	mems, scores, err := e.store.SearchMemories(ctx, ftsQuery, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("searching memories: %w", err)
	}

	// Apply filter_tags (hard AND filter).
	if len(opts.FilterTags) > 0 {
		mems, scores = filterByTags(mems, scores, opts.FilterTags)
	}

	// Apply tag boost and recency decay.
	entries := make([]memory.Entry, len(mems))
	for i, m := range mems {
		score := -scores[i] // BM25 returns negative scores; negate for ranking.
		score = applyTagBoost(score, m.Tags, opts.Tags)
		score = applyRecencyDecay(score, m.UpdatedAt)
		entries[i] = toEntry(m)
		entries[i].Score = score
	}

	// Sort by score descending.
	sortByScore(entries)

	// Apply limit.
	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
}

func toEntry(m sqlite.Memory) memory.Entry {
	return memory.Entry{
		ID:        m.ID,
		Title:     m.Title,
		Content:   m.Content,
		Tags:      m.Tags,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// Compile-time interface check.
var _ memory.MemoryEngine = (*Engine)(nil)
