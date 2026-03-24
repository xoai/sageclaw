package mcp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/tool"
)

// Manager owns all MCP server connections, provides health checks,
// auto-reconnection, and runtime add/remove.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*Client // keyed by server name
	configs map[string]MCPServerConfig
	toolReg *tool.Registry

	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a new MCP Manager.
func NewManager(toolReg *tool.Registry) *Manager {
	return &Manager{
		clients: make(map[string]*Client),
		configs: make(map[string]MCPServerConfig),
		toolReg: toolReg,
	}
}

// StartAll connects to all configured MCP servers.
// Failures are logged but do not stop other servers from connecting.
func (m *Manager) StartAll(ctx context.Context, servers map[string]MCPServerConfig) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	for name, cfg := range servers {
		if cfg.Enabled != nil && !*cfg.Enabled {
			log.Printf("mcp-manager: %s disabled, skipping", name)
			continue
		}
		if err := m.addServer(name, cfg); err != nil {
			log.Printf("mcp-manager: %s failed to start: %v", name, err)
		}
	}

	// Start health check loop.
	go m.healthLoop()
}

// AddServer adds and connects a new MCP server at runtime.
func (m *Manager) AddServer(name string, cfg MCPServerConfig) error {
	m.mu.Lock()
	if _, exists := m.clients[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("mcp server %q already exists", name)
	}
	m.mu.Unlock()

	return m.addServer(name, cfg)
}

// RemoveServer disconnects and removes an MCP server.
func (m *Manager) RemoveServer(name string) error {
	m.mu.Lock()
	client, ok := m.clients[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("mcp server %q not found", name)
	}
	delete(m.clients, name)
	delete(m.configs, name)
	m.mu.Unlock()

	client.Stop()
	log.Printf("mcp-manager: %s removed", name)
	return nil
}

// GetClient returns a client by name.
func (m *Manager) GetClient(name string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clients[name]
	return c, ok
}

// ListServers returns the status of all managed servers.
func (m *Manager) ListServers() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var servers []ServerStatus
	for name, client := range m.clients {
		cfg := m.configs[name]
		servers = append(servers, ServerStatus{
			Name:      name,
			Transport: cfg.Transport,
			Healthy:   client.Healthy(),
			ToolCount: len(client.Tools()),
			Trust:     client.Trust(),
		})
	}
	return servers
}

// ServerStatus is the public status of an MCP server connection.
type ServerStatus struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Healthy   bool   `json:"healthy"`
	ToolCount int    `json:"tool_count"`
	Trust     string `json:"trust"`
}

// Stop shuts down all MCP connections and the health loop.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		client.Stop()
		log.Printf("mcp-manager: %s stopped", name)
	}
	m.clients = make(map[string]*Client)
	m.configs = make(map[string]MCPServerConfig)
}

func (m *Manager) addServer(name string, cfg MCPServerConfig) error {
	client, err := NewClientFromConfig(name, cfg)
	if err != nil {
		return err
	}

	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := client.Start(connectCtx); err != nil {
		client.Stop()
		return fmt.Errorf("mcp %s: start failed: %w", name, err)
	}

	// Register tools into the shared registry.
	client.RegisterTools(m.toolReg)

	m.mu.Lock()
	m.clients[name] = client
	m.configs[name] = cfg
	m.mu.Unlock()

	log.Printf("mcp-manager: %s connected (%s, %d tools)", name, cfg.Transport, len(client.Tools()))
	return nil
}

func (m *Manager) healthLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkHealth()
		}
	}
}

func (m *Manager) checkHealth() {
	m.mu.RLock()
	var unhealthy []string
	for name, client := range m.clients {
		if !client.Healthy() {
			unhealthy = append(unhealthy, name)
		}
	}
	m.mu.RUnlock()

	for _, name := range unhealthy {
		m.mu.RLock()
		cfg, ok := m.configs[name]
		m.mu.RUnlock()
		if !ok {
			continue
		}

		log.Printf("mcp-manager: %s unhealthy, attempting reconnect", name)
		m.reconnect(name, cfg)
	}
}

func (m *Manager) reconnect(name string, cfg MCPServerConfig) {
	// Stop the old client.
	m.mu.Lock()
	if old, ok := m.clients[name]; ok {
		old.Stop()
		delete(m.clients, name)
	}
	m.mu.Unlock()

	// Create and connect a new client.
	client, err := NewClientFromConfig(name, cfg)
	if err != nil {
		log.Printf("mcp-manager: %s reconnect failed (config): %v", name, err)
		return
	}

	connectCtx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	if err := client.Start(connectCtx); err != nil {
		client.Stop()
		log.Printf("mcp-manager: %s reconnect failed: %v", name, err)
		return
	}

	// Re-register tools.
	client.RegisterTools(m.toolReg)

	m.mu.Lock()
	m.clients[name] = client
	m.configs[name] = cfg
	m.mu.Unlock()

	log.Printf("mcp-manager: %s reconnected (%d tools)", name, len(client.Tools()))
}
