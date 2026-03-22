package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// ToolFunc is the execution signature for all tools.
type ToolFunc func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error)

// registeredTool holds a tool's definition and implementation.
type registeredTool struct {
	def  canonical.ToolDef
	exec ToolFunc
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

// Register adds a tool to the registry.
func (r *Registry) Register(name, description string, schema json.RawMessage, fn ToolFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = registeredTool{
		def: canonical.ToolDef{
			Name:        name,
			Description: description,
			InputSchema: schema,
		},
		exec: fn,
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
