package agent

import (
	"sort"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

// LoopPool manages a pool of agent loops keyed by agent ID.
// Loops are created lazily on first access from stored configs.
type LoopPool struct {
	mu           sync.RWMutex
	loops        map[string]*Loop
	configs      map[string]Config
	provider     provider.Provider
	toolRegistry *tool.Registry
	preContext   middleware.Middleware
	postTool     middleware.Middleware
	onEvent      EventHandler
	opts         []LoopOption
}

// NewLoopPool creates a pool that lazily creates loops per agent.
func NewLoopPool(
	configs map[string]Config,
	prov provider.Provider,
	toolReg *tool.Registry,
	preContext middleware.Middleware,
	postTool middleware.Middleware,
	onEvent EventHandler,
	opts ...LoopOption,
) *LoopPool {
	cp := make(map[string]Config, len(configs))
	for k, v := range configs {
		cp[k] = v
	}
	return &LoopPool{
		loops:        make(map[string]*Loop),
		configs:      cp,
		provider:     prov,
		toolRegistry: toolReg,
		preContext:    preContext,
		postTool:     postTool,
		onEvent:      onEvent,
		opts:         opts,
	}
}

// Get returns the loop for an agent, creating it lazily if needed.
// Returns nil if no config exists for the agent.
func (p *LoopPool) Get(agentID string) *Loop {
	p.mu.RLock()
	if l, ok := p.loops[agentID]; ok {
		p.mu.RUnlock()
		return l
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if l, ok := p.loops[agentID]; ok {
		return l
	}

	cfg, ok := p.configs[agentID]
	if !ok {
		return nil
	}

	l := NewLoop(cfg, p.provider, p.toolRegistry, p.preContext, p.postTool, p.onEvent, p.opts...)
	p.loops[agentID] = l
	return l
}

// UpdateConfig replaces the config for an agent and recreates its loop.
func (p *LoopPool) UpdateConfig(agentID string, cfg Config) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configs[agentID] = cfg
	// Replace loop so the next Get uses the new config.
	delete(p.loops, agentID)
}

// RemoveConfig removes an agent's config and loop.
func (p *LoopPool) RemoveConfig(agentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.configs, agentID)
	delete(p.loops, agentID)
}

// GetConfig returns a copy of the config for an agent without creating a Loop.
// Returns nil if no config exists.
func (p *LoopPool) GetConfig(agentID string) *Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cfg, ok := p.configs[agentID]
	if !ok {
		return nil
	}
	return &cfg
}

// AgentIDs returns a sorted list of all agent IDs that have configs loaded.
func (p *LoopPool) AgentIDs() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ids := make([]string, 0, len(p.configs))
	for id := range p.configs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// NewTaskLoop creates a fresh, ephemeral Loop for a member agent task execution.
// Unlike Get(), this always creates a new Loop (not pooled/cached).
// Used by TeamExecutor to run isolated task sessions.
func (p *LoopPool) NewTaskLoop(agentID string) *Loop {
	cfg := p.GetConfig(agentID)
	if cfg == nil {
		return nil
	}
	return NewLoop(*cfg, p.provider, p.toolRegistry, p.preContext, p.postTool, p.onEvent, p.opts...)
}

// NewTaskLoopWithDeny creates a fresh Loop with additional tools denied.
// Used by SubagentManager to prevent recursive spawning.
func (p *LoopPool) NewTaskLoopWithDeny(agentID string, extraDeny []string) *Loop {
	cfg := p.GetConfig(agentID)
	if cfg == nil {
		return nil
	}
	cfgCopy := *cfg
	cfgCopy.ToolDeny = append(append([]string{}, cfgCopy.ToolDeny...), extraDeny...)
	return NewLoop(cfgCopy, p.provider, p.toolRegistry, p.preContext, p.postTool, p.onEvent, p.opts...)
}

// RegisterTaskLoop temporarily adds an ephemeral task loop to the pool
// so that InjectTo/InjectAll can reach it (e.g., for consent delivery).
// Returns a cleanup function that removes the loop from the pool.
func (p *LoopPool) RegisterTaskLoop(key string, loop *Loop) func() {
	p.mu.Lock()
	p.loops[key] = loop
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.loops, key)
		p.mu.Unlock()
	}
}

// InjectAll broadcasts a message to all active loops' inject channels.
// Used for consent responses where the target loop is unknown.
func (p *LoopPool) InjectAll(msg canonical.Message) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, l := range p.loops {
		l.Inject(msg)
	}
}

// InjectTo sends a message to a specific agent loop (targeted injection).
func (p *LoopPool) InjectTo(agentID string, msg canonical.Message) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if l, ok := p.loops[agentID]; ok {
		l.Inject(msg)
	}
}

// ProviderAndModel returns the provider name and model for a specific agent.
func (p *LoopPool) ProviderAndModel(agentID string) (string, string) {
	l := p.Get(agentID)
	if l == nil {
		return "", ""
	}
	return l.ProviderAndModel()
}
