package agent

import (
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
