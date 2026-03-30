package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// ToolFunc is the execution signature for all tools.
type ToolFunc func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error)

// Risk levels for tool consent.
const (
	RiskSafe      = "safe"      // No external effects (memory, audit)
	RiskModerate  = "moderate"  // File/web access (fs, web, cron)
	RiskSensitive = "sensitive" // Shell execution, delegation, MCP
)

// registeredTool holds a tool's definition and implementation.
type registeredTool struct {
	def    canonical.ToolDef
	exec   ToolFunc
	group  string // e.g. "fs", "runtime", "web", "memory", "mcp"
	risk   string // "safe", "moderate", "sensitive"
	source string // "builtin", "mcp:{server}", "skill:{name}"
}

// Registry manages available tools.
type Registry struct {
	tools map[string]registeredTool
	mu    sync.RWMutex
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]registeredTool)}
}

// Register adds a tool to the registry (backward compatible, group="other").
func (r *Registry) Register(name, description string, schema json.RawMessage, fn ToolFunc) {
	r.RegisterWithGroup(name, description, schema, "other", RiskModerate, "builtin", fn)
}

// RegisterWithGroup adds a tool with group, risk level, and source metadata.
func (r *Registry) RegisterWithGroup(name, description string, schema json.RawMessage, group, risk, source string, fn ToolFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = registeredTool{
		def: canonical.ToolDef{
			Name:        name,
			Description: description,
			InputSchema: schema,
		},
		exec:   fn,
		group:  group,
		risk:   risk,
		source: source,
	}
}

// Get returns a tool's definition and function by name.
func (r *Registry) Get(name string) (canonical.ToolDef, ToolFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return canonical.ToolDef{}, nil, false
	}
	return t.def, t.exec, true
}

// GetMeta returns a tool's group, risk, and source by name.
func (r *Registry) GetMeta(name string) (group, risk, source string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, found := r.tools[name]
	if !found {
		return "", "", "", false
	}
	return t.group, t.risk, t.source, true
}

// HasMCPTools returns true if any registered tools come from MCP servers.
func (r *Registry) HasMCPTools() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tools {
		if strings.HasPrefix(t.source, "mcp:") {
			return true
		}
	}
	return false
}

// Tool result soft trim constants.
const (
	softTrimThreshold = 4000 // Trim results larger than this (chars).
	softTrimHead      = 1500 // Chars to keep from the start.
	softTrimTail      = 1500 // Chars to keep from the end.
)

// Execute runs a tool by name with the given input.
// Results exceeding softTrimThreshold are trimmed immediately to prevent
// oversized requests to the LLM API (matching GoClaw's approach).
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (*canonical.ToolResult, error) {
	_, fn, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	result, err := fn(ctx, input)
	if err != nil {
		return result, err
	}
	if result != nil {
		result.Content = softTrimResult(result.Content)
	}
	return result, nil
}

// softTrimResult trims a tool result string if it exceeds the threshold.
// Keeps the first 1500 and last 1500 chars with a trim note in between.
func softTrimResult(content string) string {
	if len(content) <= softTrimThreshold {
		return content
	}
	removed := len(content) - softTrimHead - softTrimTail
	return content[:softTrimHead] +
		fmt.Sprintf("\n\n[... %d chars trimmed — use read_file for full content ...]\n\n", removed) +
		content[len(content)-softTrimTail:]
}

// List returns all registered tool definitions.
func (r *Registry) List() []canonical.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]canonical.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.def)
	}
	return defs
}

// ListForAgent returns tool definitions filtered by profile, deny list, and MCP allowlist.
// Profile controls visibility — only tools in the profile's groups are sent to the LLM.
// This reduces token usage: a messaging agent gets ~9 tools instead of ~46.
// Deny list is an additional hard block (tool not shown to LLM).
// allowedMCPServers filters MCP tools: if non-nil, only listed servers pass through.
// Schemas are compressed to minimize token usage (property descriptions stripped).
func (r *Registry) ListForAgent(profile string, deny []string, allowedMCPServers []string) []canonical.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Resolve profile groups for visibility filtering.
	profileGroups := ProfileGroups(profile) // nil = all groups (full profile)

	// Step 1: Start with tools visible to this profile.
	candidates := make(map[string]registeredTool)
	for name, t := range r.tools {
		// Profile visibility filter: only include tools whose group is in the profile.
		// "full" profile (nil groups) includes everything.
		// MCP tools always pass through profile filter (filtered by allowlist instead).
		if profileGroups != nil && !strings.HasPrefix(t.source, "mcp:") {
			if !profileGroups[t.group] {
				continue
			}
		}
		candidates[name] = t
	}

	// Step 2: Remove denied tools/groups (hard block).
	for _, d := range deny {
		if strings.HasPrefix(d, "group:") {
			groupName := strings.TrimPrefix(d, "group:")
			for name, t := range candidates {
				if t.group == groupName {
					delete(candidates, name)
				}
			}
		} else {
			delete(candidates, d)
		}
	}

	// Step 3: Filter MCP tools by allowed server list.
	if allowedMCPServers != nil {
		allowed := make(map[string]bool, len(allowedMCPServers))
		for _, s := range allowedMCPServers {
			allowed[s] = true
		}
		for name, t := range candidates {
			if strings.HasPrefix(t.source, "mcp:") {
				server := strings.TrimPrefix(t.source, "mcp:")
				if !allowed[server] {
					delete(candidates, name)
				}
			}
		}
	}

	// Step 4: Build definitions — keep full schemas so LLM sees parameter descriptions.
	defs := make([]canonical.ToolDef, 0, len(candidates))
	for _, t := range candidates {
		defs = append(defs, t.def)
	}
	return defs
}

// compressSchema strips property-level "description" fields from JSON schemas
// to reduce token usage. Keeps type, required, enum, and structure intact.
// Saves ~40-60% of schema tokens without losing structural information.
func compressSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return schema
	}

	var obj map[string]any
	if err := json.Unmarshal(schema, &obj); err != nil {
		return schema // unparseable — return as-is
	}

	stripDescriptions(obj)

	compressed, err := json.Marshal(obj)
	if err != nil {
		return schema
	}
	return compressed
}

// stripDescriptions recursively removes "description" fields from
// property definitions within a JSON schema. Preserves the top-level
// structure and all type/required/enum information.
func stripDescriptions(obj map[string]any) {
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return
	}

	for _, v := range props {
		prop, ok := v.(map[string]any)
		if !ok {
			continue
		}
		delete(prop, "description")
		delete(prop, "default")

		// Recurse into nested objects.
		if nested, ok := prop["properties"].(map[string]any); ok {
			stripDescriptions(map[string]any{"properties": nested})
		}
		// Recurse into array items.
		if items, ok := prop["items"].(map[string]any); ok {
			stripDescriptions(items)
		}
	}
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// UnregisterBySource removes all tools matching the given source prefix.
// For example, UnregisterBySource("mcp:brave-search") removes all tools
// registered by the brave-search MCP server.
func (r *Registry) UnregisterBySource(source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, t := range r.tools {
		if t.source == source {
			delete(r.tools, name)
		}
	}
}

// Names returns all registered tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}
