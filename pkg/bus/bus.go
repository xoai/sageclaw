package bus

import (
	"context"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// Envelope wraps messages for transport through the bus.
type Envelope struct {
	SessionID string
	AgentID   string
	Channel   string
	ChatID    string
	Kind      string // "dm" or "group"
	ThreadID  string // Thread/topic ID (empty = none)
	Mentioned bool   // Was bot @mentioned? (relevant for groups)
	Messages  []canonical.Message
	Metadata  map[string]string
}

// MessageBus is the process boundary interface for message routing (ADR-013).
type MessageBus interface {
	PublishInbound(ctx context.Context, env Envelope) error
	SubscribeInbound(ctx context.Context, handler func(Envelope)) error
	PublishOutbound(ctx context.Context, env Envelope) error
	SubscribeOutbound(ctx context.Context, handler func(Envelope)) error
}
