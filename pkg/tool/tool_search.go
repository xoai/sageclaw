package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/agent/context/deferred"
	"github.com/xoai/sageclaw/pkg/canonical"
)

// RegisterToolSearch registers the tool_search meta-tool that lets the LLM
// discover and load deferred tools by keyword search.
func RegisterToolSearch(reg *Registry) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Keyword to search for in tool names and descriptions"
			}
		},
		"required": ["query"]
	}`)

	reg.RegisterFull("tool_search", "Search for available tools by keyword. "+
		"Returns full tool schemas for matching tools. Use when you need a tool "+
		"that isn't in your current tool list.",
		schema, GroupCore, RiskSafe, "builtin", true,
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			var params struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return &canonical.ToolResult{Content: "Invalid input: " + err.Error(), IsError: true}, nil
			}
			if params.Query == "" {
				return &canonical.ToolResult{Content: "Query is required", IsError: true}, nil
			}

			// Get all tools from registry for searching.
			allTools := reg.List()
			results := deferred.SearchTools(allTools, params.Query, 5)

			if len(results) == 0 {
				return &canonical.ToolResult{Content: fmt.Sprintf("No tools found matching %q", params.Query)}, nil
			}

			// Format results with full schemas.
			out, _ := json.MarshalIndent(results, "", "  ")
			return &canonical.ToolResult{Content: string(out)}, nil
		},
	)
}
