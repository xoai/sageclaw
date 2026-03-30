package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// RegisterGraph registers memory graph tools (ontology + search).
func RegisterGraph(reg *Registry, graph memory.GraphEngine, engine ...memory.MemoryEngine) {
	reg.RegisterWithGroup("memory_link", "Create a typed relationship between two memories",
		json.RawMessage(`{"type":"object","properties":{"source_id":{"type":"string","description":"Source memory ID"},"target_id":{"type":"string","description":"Target memory ID"},"relation":{"type":"string","description":"Relationship type (e.g. depends_on, contains, owned_by)"}},"required":["source_id","target_id","relation"]}`),
		GroupGraph, RiskSafe, "builtin", memoryLink(graph))

	reg.RegisterWithGroup("memory_graph", "Traverse relationships from a memory",
		json.RawMessage(`{"type":"object","properties":{"start_id":{"type":"string","description":"Starting memory ID"},"direction":{"type":"string","description":"Traversal direction: outbound, inbound, or both","default":"outbound"},"depth":{"type":"integer","description":"Max traversal depth (1-5, default 2)","default":2}},"required":["start_id"]}`),
		GroupGraph, RiskSafe, "builtin", memoryGraph(graph))

	// Register knowledge graph search if memory engine is available.
	if len(engine) > 0 && engine[0] != nil {
		reg.RegisterWithGroup("knowledge_graph_search", "Search the knowledge graph for entities by name or description",
			json.RawMessage(`{"type":"object","properties":{`+
				`"query":{"type":"string","description":"Search query (entity name or description)"},`+
				`"type":{"type":"string","description":"Entity type filter (e.g. person, project, concept)"},`+
				`"max_results":{"type":"integer","description":"Maximum results (default 5)"}`+
				`},"required":["query"]}`),
			GroupGraph, RiskSafe, "builtin", knowledgeGraphSearch(engine[0]))
	}
}

func memoryLink(graph memory.GraphEngine) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			SourceID string         `json:"source_id"`
			TargetID string         `json:"target_id"`
			Relation string         `json:"relation"`
			Props    map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		id, err := graph.Link(ctx, params.SourceID, params.TargetID, params.Relation, params.Props)
		if err != nil {
			return errorResult("link failed: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Edge created: %s -[%s]-> %s (id: %s)",
			params.SourceID[:8], params.Relation, params.TargetID[:8], id[:8])}, nil
	}
}

func memoryGraph(graph memory.GraphEngine) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			StartID   string `json:"start_id"`
			Direction string `json:"direction"`
			Depth     int    `json:"depth"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if params.Direction == "" {
			params.Direction = "outbound"
		}
		if params.Depth == 0 {
			params.Depth = 2
		}

		entries, edges, err := graph.Graph(ctx, params.StartID, params.Direction, params.Depth)
		if err != nil {
			return errorResult("graph traversal failed: " + err.Error()), nil
		}

		if len(entries) == 0 {
			return &canonical.ToolResult{Content: "No connected memories found."}, nil
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "## Graph from %s (%s, depth %d)\n\n", params.StartID[:8], params.Direction, params.Depth)

		fmt.Fprintf(&sb, "### Nodes (%d)\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&sb, "- **%s** %s", e.ID[:8], e.Title)
			if len(e.Tags) > 0 {
				fmt.Fprintf(&sb, " [%s]", strings.Join(e.Tags, ", "))
			}
			fmt.Fprintf(&sb, "\n")
		}

		fmt.Fprintf(&sb, "\n### Edges (%d)\n", len(edges))
		for _, e := range edges {
			fmt.Fprintf(&sb, "- %s -[%s]-> %s\n", e.SourceID[:8], e.Relation, e.TargetID[:8])
		}

		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func knowledgeGraphSearch(engine memory.MemoryEngine) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Query      string `json:"query"`
			Type       string `json:"type"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}
		if params.MaxResults <= 0 {
			params.MaxResults = 5
		}

		opts := memory.SearchOptions{
			Limit: params.MaxResults,
		}
		if params.Type != "" {
			opts.FilterTags = []string{params.Type}
		}

		entries, err := engine.Search(ctx, params.Query, opts)
		if err != nil {
			return errorResult("search failed: " + err.Error()), nil
		}
		if len(entries) == 0 {
			return &canonical.ToolResult{Content: "No matching entities found."}, nil
		}

		var sb strings.Builder
		for _, e := range entries {
			fmt.Fprintf(&sb, "- **%s** (%.2f)\n  %s\n", e.Title, e.Score, truncateContent(e.Content, 200))
			if len(e.Tags) > 0 {
				fmt.Fprintf(&sb, "  Tags: %s\n", strings.Join(e.Tags, ", "))
			}
			sb.WriteString("\n")
		}
		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func truncateContent(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
