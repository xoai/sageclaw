package memory

import "context"

// Edge represents a typed directed relationship between two memories.
type Edge struct {
	ID         string
	SourceID   string
	TargetID   string
	Relation   string
	Properties map[string]any
}

// GraphEngine extends memory with typed relationships.
type GraphEngine interface {
	Link(ctx context.Context, sourceID, targetID, relation string, props map[string]any) (string, error)
	Unlink(ctx context.Context, sourceID, targetID, relation string) error
	Graph(ctx context.Context, startID string, direction string, depth int) ([]Entry, []Edge, error)
}
