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

// Execute runs a tool by name with the given input.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (*canonical.ToolResult, error) {
	_, fn, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return fn(ctx, input)
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

// ListForAgent returns tool definitions filtered by profile, deny, and alsoAllow.
// Resolution: start with profile set → intersect with enabled (if non-empty) →
// remove deny → add back alsoAllow.
func (r *Registry) ListForAgent(profile string, enabled, deny, alsoAllow []string) []canonical.ToolDef {
	if profile == "" {
		profile = ProfileFull
	}

	// Step 1: Get profile's allowed groups.
	allowedGroups := ProfileGroups(profile)

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Step 2: Collect tools matching profile groups.
	// "full" profile means all tools.
	candidates := make(map[string]registeredTool)
	for name, t := range r.tools {
		if profile == ProfileFull {
			candidates[name] = t
		} else if allowedGroups[t.group] {
			candidates[name] = t
		}
	}

	// Step 3: Intersect with enabled list (backward compat).
	if len(enabled) > 0 {
		enabledSet := toSet(enabled)
		for name := range candidates {
			if !enabledSet[name] {
				delete(candidates, name)
			}
		}
	}

	// Step 4: Remove denied tools/groups.
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

	// Step 5: Add back alsoAllow tools.
	for _, a := range alsoAllow {
		if strings.HasPrefix(a, "group:") {
			groupName := strings.TrimPrefix(a, "group:")
			for name, t := range r.tools {
				if t.group == groupName {
					candidates[name] = t
				}
			}
		} else if t, ok := r.tools[a]; ok {
			candidates[a] = t
		}
	}

	defs := make([]canonical.ToolDef, 0, len(candidates))
	for _, t := range candidates {
		defs = append(defs, t.def)
	}
	return defs
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
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

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
