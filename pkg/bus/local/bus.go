package local

import (
	"context"
	"sync"

	"github.com/xoai/sageclaw/pkg/bus"
)

// Bus is an in-memory message bus implementation.
type Bus struct {
	inboundHandlers  []func(bus.Envelope)
	outboundHandlers []func(bus.Envelope)
	mu               sync.RWMutex
}

// New creates a new in-memory bus.
func New() *Bus {
	return &Bus{}
}

func (b *Bus) PublishInbound(_ context.Context, env bus.Envelope) error {
	b.mu.RLock()
	handlers := make([]func(bus.Envelope), len(b.inboundHandlers))
	copy(handlers, b.inboundHandlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		go h(env)
	}
	return nil
}

func (b *Bus) SubscribeInbound(_ context.Context, handler func(bus.Envelope)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inboundHandlers = append(b.inboundHandlers, handler)
	return nil
}

func (b *Bus) PublishOutbound(_ context.Context, env bus.Envelope) error {
	b.mu.RLock()
	handlers := make([]func(bus.Envelope), len(b.outboundHandlers))
	copy(handlers, b.outboundHandlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		go h(env)
	}
	return nil
}

func (b *Bus) SubscribeOutbound(ctx context.Context, handler func(bus.Envelope)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Wrap handler with context check — when context is canceled,
	// the handler becomes a no-op. This enables channel hot-reload
	// without accumulating dead subscribers.
	b.outboundHandlers = append(b.outboundHandlers, func(env bus.Envelope) {
		if ctx.Err() != nil {
			return // Context canceled — this subscriber is dead.
		}
		handler(env)
	})
	return nil
}

// Compile-time check.
var _ bus.MessageBus = (*Bus)(nil)
