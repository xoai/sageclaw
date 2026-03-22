package channel

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/xoai/sageclaw/pkg/bus"
)

// Factory creates a channel adapter from config.
type Factory func(config map[string]string) (Channel, error)

// Manager handles dynamic channel lifecycle — start, stop, and hot-reload.
type Manager struct {
	mu        sync.RWMutex
	channels  map[string]Channel // name → running channel
	factories map[string]Factory // type → factory function
	msgBus    bus.MessageBus
	ctx       context.Context
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

// RegisterFactory registers a channel type factory.
func (m *Manager) RegisterFactory(channelType string, factory Factory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[channelType] = factory
}

// StartChannel creates and starts a channel from config.
// If a channel with the same name is already running, it's stopped first.
func (m *Manager) StartChannel(channelType string, config map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing channel of same type if running.
	if existing, ok := m.channels[channelType]; ok {
		log.Printf("channel-manager: stopping existing %s channel", channelType)
		existing.Stop(m.ctx)
		delete(m.channels, channelType)
	}

	// Find factory.
	factory, ok := m.factories[channelType]
	if !ok {
		return fmt.Errorf("unknown channel type: %s", channelType)
	}

	// Create and start.
	ch, err := factory(config)
	if err != nil {
		return fmt.Errorf("creating %s channel: %w", channelType, err)
	}

	if err := ch.Start(m.ctx, m.msgBus); err != nil {
		return fmt.Errorf("starting %s channel: %w", channelType, err)
	}

	m.channels[channelType] = ch
	log.Printf("channel-manager: %s started (hot-reload)", channelType)
	return nil
}

// StopChannel stops a running channel.
func (m *Manager) StopChannel(channelType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch, ok := m.channels[channelType]
	if !ok {
		return fmt.Errorf("channel %s not running", channelType)
	}

	ch.Stop(m.ctx)
	delete(m.channels, channelType)
	log.Printf("channel-manager: %s stopped", channelType)
	return nil
}

// IsRunning checks if a channel type is currently running.
func (m *Manager) IsRunning(channelType string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.channels[channelType]
	return ok
}

// Running returns all running channel names.
func (m *Manager) Running() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

// Register adds an already-started channel (for channels started at boot).
func (m *Manager) Register(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.Name()] = ch
}
