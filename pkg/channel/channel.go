package channel

import (
	"context"

	"github.com/xoai/sageclaw/pkg/bus"
)

// Channel defines the interface for messaging platforms.
type Channel interface {
	Name() string
	Start(ctx context.Context, msgBus bus.MessageBus) error
	Stop(ctx context.Context) error
}
