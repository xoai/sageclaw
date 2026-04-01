// Package deferred implements deferred tool loading for the v2 context pipeline.
// Tools are split into always-loaded (full schema) and deferred (name-only stubs).
// The LLM resolves deferred tools on demand via the tool_search meta-tool.
package deferred

import (
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// AlwaysLoadTools is the set of core tools that are always fully loaded.
// These are essential for basic agent operation and should never be deferred.
var AlwaysLoadTools = map[string]bool{
	// Core.
	"datetime":    true,
	"load_skill":  true,
	"plan":        true,
	"tool_search": true,

	// File system (most common operations).
	"read_file":  true,
	"write_file": true,
	"edit_file":  true,
	"glob":       true,
	"grep":       true,
	"list_files": true,

	// Runtime.
	"execute_command": true,

	// Orchestration.
	"delegate": true,
}

// ToolDefStub is a lightweight tool reference (name + one-line description)
// sent to the LLM as a hint of available tools. No schema is included.
type ToolDefStub struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// FilterDeferred splits tools into always-loaded (full schemas) and deferred
// (name + description stubs). The alwaysLoad map overrides the default
// AlwaysLoadTools set — pass nil to use defaults.
func FilterDeferred(tools []canonical.ToolDef, alwaysLoad map[string]bool) (loaded []canonical.ToolDef, stubs []ToolDefStub) {
	if alwaysLoad == nil {
		alwaysLoad = AlwaysLoadTools
	}

	for _, t := range tools {
		if alwaysLoad[t.Name] {
			loaded = append(loaded, t)
		} else {
			// Truncate description to first sentence for the stub.
			desc := t.Description
			if idx := strings.Index(desc, ". "); idx > 0 && idx < 120 {
				desc = desc[:idx+1]
			} else if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			stubs = append(stubs, ToolDefStub{Name: t.Name, Description: desc})
		}
	}
	return
}

// SearchTools performs keyword search over tool names and descriptions.
// Returns full ToolDef schemas for the top maxResults matches.
func SearchTools(allTools []canonical.ToolDef, query string, maxResults int) []canonical.ToolDef {
	if maxResults <= 0 {
		maxResults = 5
	}
	query = strings.ToLower(query)
	keywords := strings.Fields(query)

	type scored struct {
		tool  canonical.ToolDef
		score int
	}

	var matches []scored
	for _, t := range allTools {
		nameLower := strings.ToLower(t.Name)
		descLower := strings.ToLower(t.Description)

		score := 0
		for _, kw := range keywords {
			// Exact name match scores highest.
			if nameLower == kw {
				score += 10
			} else if strings.Contains(nameLower, kw) {
				score += 5
			}
			if strings.Contains(descLower, kw) {
				score += 2
			}
		}

		if score > 0 {
			matches = append(matches, scored{tool: t, score: score})
		}
	}

	// Sort by score descending (simple bubble — small N).
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].score > matches[i].score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	var result []canonical.ToolDef
	for i := 0; i < len(matches) && i < maxResults; i++ {
		result = append(result, matches[i].tool)
	}
	return result
}

// StubsPromptSection formats deferred tool stubs as a system prompt injection.
func StubsPromptSection(stubs []ToolDefStub) string {
	if len(stubs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Additional tools available via tool_search:\n")
	for _, s := range stubs {
		b.WriteString("  ")
		b.WriteString(s.Name)
		b.WriteString(" — ")
		b.WriteString(s.Description)
		b.WriteString("\n")
	}
	return b.String()
}
