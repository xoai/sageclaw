package agentcfg

import "sync"

// Provider gives read access to agent configurations.
// Consumers use this interface instead of loading configs directly.
type Provider interface {
	// Get returns the config for an agent, or nil if not found.
	Get(agentID string) *AgentConfig

	// List returns all loaded agent configs.
	List() map[string]*AgentConfig

	// ServesChannel reports whether an agent is allowed to serve
	// a given channel type. Returns true if the agent's Channels.Serve
	// list is empty (backward compat: serve all) or contains the type.
	// Returns true for unknown agents (permissive default).
	ServesChannel(agentID, channelType string) bool
}

// MapProvider implements Provider backed by an in-memory map.
// Thread-safe for concurrent reads and updates from the file watcher.
type MapProvider struct {
	mu      sync.RWMutex
	configs map[string]*AgentConfig
}

// NewMapProvider creates a Provider from an existing configs map.
// The map is copied so the caller can continue to use the original.
func NewMapProvider(configs map[string]*AgentConfig) *MapProvider {
	cp := make(map[string]*AgentConfig, len(configs))
	for k, v := range configs {
		cp[k] = v
	}
	return &MapProvider{configs: cp}
}

func (p *MapProvider) Get(agentID string) *AgentConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.configs[agentID]
}

func (p *MapProvider) List() map[string]*AgentConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := make(map[string]*AgentConfig, len(p.configs))
	for k, v := range p.configs {
		cp[k] = v
	}
	return cp
}

func (p *MapProvider) ServesChannel(agentID, channelType string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cfg, ok := p.configs[agentID]
	if !ok {
		return true // Unknown agent — permissive default.
	}

	serve := cfg.Channels.Serve
	if len(serve) == 0 {
		return true // Empty = serve all channels.
	}

	for _, s := range serve {
		if s == channelType {
			return true
		}
	}
	return false
}

// Update adds or replaces an agent config (called by file watcher).
func (p *MapProvider) Update(id string, cfg *AgentConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configs[id] = cfg
}

// Remove deletes an agent config (called on agent folder deletion).
func (p *MapProvider) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.configs, id)
}
