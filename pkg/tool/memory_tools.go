package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// RegisterMemory registers memory tools on the registry.
func RegisterMemory(reg *Registry, engine memory.MemoryEngine) {
	reg.RegisterWithGroup("memory_search", "Search stored memories by natural language query",
		json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"tags":{"type":"array","items":{"type":"string"},"description":"Tags to boost in results"},"filter_tags":{"type":"array","items":{"type":"string"},"description":"Only return memories with ALL these tags"},"limit":{"type":"integer","description":"Max results (default 10)"}},"required":["query"]}`),
		GroupMemory, RiskSafe, "builtin", memorySearch(engine))

	reg.RegisterWithGroup("memory_get", "Retrieve a specific memory by ID",
		json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Memory ID"}},"required":["id"]}`),
		GroupMemory, RiskSafe, "builtin", memoryGet(engine))
}

func memorySearch(engine memory.MemoryEngine) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Query      string   `json:"query"`
			Tags       []string `json:"tags"`
			FilterTags []string `json:"filter_tags"`
			Limit      int      `json:"limit"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		results, err := engine.Search(ctx, params.Query, memory.SearchOptions{
			Tags:       params.Tags,
			FilterTags: params.FilterTags,
			Limit:      params.Limit,
		})
		if err != nil {
			return errorResult("search failed: " + err.Error()), nil
		}

		if len(results) == 0 {
			return &canonical.ToolResult{Content: "No memories found."}, nil
		}

		var sb strings.Builder
		for i, entry := range results {
			fmt.Fprintf(&sb, "## Memory %d (score: %.2f)\n", i+1, entry.Score)
			fmt.Fprintf(&sb, "**ID:** %s\n", entry.ID)
			if entry.Title != "" {
				fmt.Fprintf(&sb, "**Title:** %s\n", entry.Title)
			}
			if len(entry.Tags) > 0 {
				fmt.Fprintf(&sb, "**Tags:** %s\n", strings.Join(entry.Tags, ", "))
			}
			fmt.Fprintf(&sb, "\n%s\n\n", entry.Content)
		}

		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func memoryGet(engine memory.MemoryEngine) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Use List with limit=1 and search by ID pattern.
		// Since MemoryEngine doesn't have a GetByID, we use List and filter.
		entries, err := engine.List(ctx, nil, 100, 0)
		if err != nil {
			return errorResult("list failed: " + err.Error()), nil
		}

		for _, entry := range entries {
			if entry.ID == params.ID {
				var sb strings.Builder
				fmt.Fprintf(&sb, "**ID:** %s\n", entry.ID)
				if entry.Title != "" {
					fmt.Fprintf(&sb, "**Title:** %s\n", entry.Title)
				}
				if len(entry.Tags) > 0 {
					fmt.Fprintf(&sb, "**Tags:** %s\n", strings.Join(entry.Tags, ", "))
				}
				fmt.Fprintf(&sb, "**Created:** %s\n", entry.CreatedAt.Format("2006-01-02 15:04:05"))
				fmt.Fprintf(&sb, "\n%s", entry.Content)
				return &canonical.ToolResult{Content: sb.String()}, nil
			}
		}

		return errorResult(fmt.Sprintf("memory not found: %s", params.ID)), nil
	}
}
