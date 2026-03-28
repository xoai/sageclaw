package channel

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/xoai/sageclaw/pkg/bus"
)

// Factory creates a channel adapter from config.
// Config must include "__conn_id" for the connection ID.
type Factory func(config map[string]string) (Channel, error)

// Manager handles dynamic channel lifecycle — start, stop, and hot-reload.
type Manager struct {
	mu        sync.RWMutex
	channels  map[string]Channel // connection ID → running channel
	factories map[string]Factory // platform type → factory function
	msgBus    bus.MessageBus
	ctx       context.Context
	mux       *http.ServeMux // shared webhook mux (set by RPC server)
}

// NewManager creates a channel manager.
func NewManager(ctx context.Context, msgBus bus.MessageBus) *Manager {
	return &Manager{
		channels:  make(map[string]Channel),
		factories: make(map[string]Factory),
		msgBus:    msgBus,
		ctx:       ctx,
	}
}

// SetWebhookMux sets the shared HTTP mux for webhook registration.
// Called by the RPC server after creating its mux. Channels started after
// this call will have their webhooks registered automatically.
func (m *Manager) SetWebhookMux(mux *http.ServeMux) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mux = mux
	// Register webhooks for already-running channels.
	for _, ch := range m.channels {
		if wr, ok := ch.(WebhookRegistrar); ok {
			wr.RegisterWebhook(mux)
		}
	}
}

// RegisterFactory registers a platform type factory.
func (m *Manager) RegisterFactory(platformType string, factory Factory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[platformType] = factory
}

// StartConnection creates and starts a channel for a specific connection.
// If a channel with the same connection ID is already running, it's stopped first.
func (m *Manager) StartConnection(connID, platformType string, config map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing connection if running.
	if existing, ok := m.channels[connID]; ok {
		log.Printf("channel-manager: stopping existing connection %s", connID)
		existing.Stop(m.ctx)
		delete(m.channels, connID)
	}

	// Find factory.
	factory, ok := m.factories[platformType]
	if !ok {
		return fmt.Errorf("unknown platform type: %s", platformType)
	}

	// Inject connection ID into config for the factory.
	config["__conn_id"] = connID

	// Create and start.
	ch, err := factory(config)
	if err != nil {
		return fmt.Errorf("creating %s connection %s: %w", platformType, connID, err)
	}

	if err := ch.Start(m.ctx, m.msgBus); err != nil {
		return fmt.Errorf("starting %s connection %s: %w", platformType, connID, err)
	}

	// Register webhook routes if the channel needs them.
	if wr, ok := ch.(WebhookRegistrar); ok && m.mux != nil {
		wr.RegisterWebhook(m.mux)
	}

	m.channels[connID] = ch
	log.Printf("channel-manager: connection %s (%s) started", connID, platformType)
	return nil
}

// StartChannel creates and starts a channel (legacy compatibility).
// Uses platform type as connection ID for backward compat.
func (m *Manager) StartChannel(channelType string, config map[string]string) error {
	connID := config["__conn_id"]
	if connID == "" {
		connID = channelType // Legacy: use type as ID
	}
	return m.StartConnection(connID, channelType, config)
}

// StopConnection stops a running connection.
func (m *Manager) StopConnection(connID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, ok := m.channels[connID]
	if !ok {
		return fmt.Errorf("connection %s not running", connID)
	}

	ch.Stop(m.ctx)
	delete(m.channels, connID)
	log.Printf("channel-manager: connection %s stopped", connID)
	return nil
}

// StopChannel stops a running channel (legacy compatibility).
func (m *Manager) StopChannel(channelType string) error {
	return m.StopConnection(channelType)
}

// IsRunning checks if a connection is currently running.
func (m *Manager) IsRunning(connID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.channels[connID]
	return ok
}

// RunningByPlatform returns connection IDs for a given platform type.
func (m *Manager) RunningByPlatform(platform string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for _, ch := range m.channels {
		if ch.Platform() == platform {
			ids = append(ids, ch.ID())
		}
	}
	return ids
}

// Running returns all running connection IDs.
func (m *Manager) Running() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.channels))
	for id := range m.channels {
		ids = append(ids, id)
	}
	return ids
}

// GetChannel returns the running channel for a connection ID, or nil.
func (m *Manager) GetChannel(connID string) Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channels[connID]
}

// ForEachChannel calls fn for each active channel.
func (m *Manager) ForEachChannel(fn func(Channel)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		fn(ch)
	}
}

// Register adds an already-started channel (for channels started at boot).
func (m *Manager) Register(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wr, ok := ch.(WebhookRegistrar); ok && m.mux != nil {
		wr.RegisterWebhook(m.mux)
	}
	m.channels[ch.ID()] = ch
}
