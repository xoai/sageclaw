package channel

import (
	"context"
	"net/http"

	"github.com/xoai/sageclaw/pkg/bus"
)

// Channel defines the interface for messaging platforms.
type Channel interface {
	ID() string       // Connection ID: "tg_abc123"
	Platform() string // Platform type: "telegram", "discord", etc.
	Start(ctx context.Context, msgBus bus.MessageBus) error
	Stop(ctx context.Context) error
}

// WebhookRegistrar is implemented by adapters that need HTTP webhook routes
// registered on the shared server mux (e.g. Zalo, WhatsApp).
type WebhookRegistrar interface {
	RegisterWebhook(mux *http.ServeMux)
}

// WebhookURLUpdater is implemented by adapters that can programmatically
// register/update their webhook URL with the platform's API.
// This is NOT the same as WebhookRegistrar (which registers local HTTP routes).
// Used by the tunnel client to auto-register webhooks when the tunnel starts.
type WebhookURLUpdater interface {
	UpdateWebhookURL(ctx context.Context, webhookURL string) error
}
