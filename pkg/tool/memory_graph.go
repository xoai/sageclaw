package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// RegisterGraph registers memory graph tools (ontology).
func RegisterGraph(reg *Registry, graph memory.GraphEngine) {
	reg.Register("memory_link", "Create a typed relationship between two memories",
		json.RawMessage(`{"type":"object","properties":{"source_id":{"type":"string","description":"Source memory ID"},"target_id":{"type":"string","description":"Target memory ID"},"relation":{"type":"string","description":"Relationship type (e.g. depends_on, contains, owned_by)"}},"required":["source_id","target_id","relation"]}`),
		memoryLink(graph))

	reg.Register("memory_graph", "Traverse relationships from a memory",
		json.RawMessage(`{"type":"object","properties":{"start_id":{"type":"string","description":"Starting memory ID"},"direction":{"type":"string","description":"Traversal direction: outbound, inbound, or both","default":"outbound"},"depth":{"type":"integer","description":"Max traversal depth (1-5, default 2)","default":2}},"required":["start_id"]}`),
		memoryGraph(graph))
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
