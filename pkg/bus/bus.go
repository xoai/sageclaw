package bus

import (
	"context"
	"strings"

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
	HasAudio  bool   // Hint: contains audio/voice content.
	Messages  []canonical.Message
	Metadata  map[string]string
}

// SanitizeThreadID ensures a thread ID is safe for use in session keys.
// Session keys use ":" as a delimiter, so thread IDs must not contain it.
// Current platforms (Telegram integers, Discord snowflakes, Slack timestamps)
// naturally satisfy this, but future adapters must call this function.
func SanitizeThreadID(threadID string) string {
	return strings.ReplaceAll(threadID, ":", "_")
}

// MessageBus is the process boundary interface for message routing (ADR-013).
type MessageBus interface {
	PublishInbound(ctx context.Context, env Envelope) error
	SubscribeInbound(ctx context.Context, handler func(Envelope)) error
	PublishOutbound(ctx context.Context, env Envelope) error
	SubscribeOutbound(ctx context.Context, handler func(Envelope)) error
}
