package channel

import (
	"context"

	"github.com/xoai/sageclaw/pkg/bus"
)

// Channel defines the interface for messaging platforms.
type Channel interface {
	ID() string       // Connection ID: "tg_abc123"
	Platform() string // Platform type: "telegram", "discord", etc.
	Start(ctx context.Context, msgBus bus.MessageBus) error
	Stop(ctx context.Context) error
}
