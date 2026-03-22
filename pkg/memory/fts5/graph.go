package fts5

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

// GraphOps implements memory.GraphEngine using SQLite.
type GraphOps struct {
	store *sqlite.Store
}

// NewGraphOps creates graph operations backed by the store.
func NewGraphOps(store *sqlite.Store) *GraphOps {
	return &GraphOps{store: store}
}

func (g *GraphOps) Link(ctx context.Context, sourceID, targetID, relation string, props map[string]any) (string, error) {
	if sourceID == targetID {
		return "", fmt.Errorf("cannot link a memory to itself")
	}

	id := newID()
	propsJSON, _ := json.Marshal(props)
	if props == nil {
		propsJSON = []byte("{}")
	}

	_, err := g.store.DB().ExecContext(ctx,
		`INSERT INTO edges (id, source_id, target_id, relation, properties) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(source_id, target_id, relation) DO UPDATE SET properties = excluded.properties`,
		id, sourceID, targetID, relation, string(propsJSON),
	)
	if err != nil {
		return "", fmt.Errorf("creating edge: %w", err)
	}
	return id, nil
}

func (g *GraphOps) Unlink(ctx context.Context, sourceID, targetID, relation string) error {
	result, err := g.store.DB().ExecContext(ctx,
		`DELETE FROM edges WHERE source_id = ? AND target_id = ? AND relation = ?`,
		sourceID, targetID, relation,
	)
	if err != nil {
		return fmt.Errorf("deleting edge: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("edge not found")
	}
	return nil
}

func (g *GraphOps) Graph(ctx context.Context, startID string, direction string, depth int) ([]memory.Entry, []memory.Edge, error) {
	if depth <= 0 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}

	visited := map[string]bool{startID: true}
	var allEdges []memory.Edge
	frontier := []string{startID}

	for d := 0; d < depth && len(frontier) > 0; d++ {
		var nextFrontier []string

		for _, nodeID := range frontier {
			edges, err := g.getEdges(ctx, nodeID, direction)
			if err != nil {
				return nil, nil, err
			}

			for _, edge := range edges {
				allEdges = append(allEdges, edge)
				// Determine the neighbor.
				neighbor := edge.TargetID
				if edge.TargetID == nodeID {
					neighbor = edge.SourceID
				}
				if !visited[neighbor] {
					visited[neighbor] = true
					nextFrontier = append(nextFrontier, neighbor)
				}
			}
		}

		frontier = nextFrontier
	}

	// Fetch all visited nodes.
	var entries []memory.Entry
	for id := range visited {
		m, err := g.store.GetMemory(ctx, id)
		if err != nil {
			continue
		}
		entries = append(entries, memory.Entry{
			ID:        m.ID,
			Title:     m.Title,
			Content:   m.Content,
			Tags:      m.Tags,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		})
	}

	return entries, allEdges, nil
}

func (g *GraphOps) getEdges(ctx context.Context, nodeID, direction string) ([]memory.Edge, error) {
	var query string
	switch direction {
	case "outbound":
		query = `SELECT id, source_id, target_id, relation, properties FROM edges WHERE source_id = ?`
	case "inbound":
		query = `SELECT id, source_id, target_id, relation, properties FROM edges WHERE target_id = ?`
	default: // "both"
		query = `SELECT id, source_id, target_id, relation, properties FROM edges WHERE source_id = ? OR target_id = ?`
	}

	var args []any
	args = append(args, nodeID)
	if direction != "outbound" && direction != "inbound" {
		args = append(args, nodeID)
	}

	rows, err := g.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying edges: %w", err)
	}
	defer rows.Close()

	var edges []memory.Edge
	for rows.Next() {
		var e memory.Edge
		var propsJSON string
		if err := rows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.Relation, &propsJSON); err != nil {
			return nil, fmt.Errorf("scanning edge: %w", err)
		}
		json.Unmarshal([]byte(propsJSON), &e.Properties)
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func newID() string {
	return sqlite.NewID()
}

var _ memory.GraphEngine = (*GraphOps)(nil)
