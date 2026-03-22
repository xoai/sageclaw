package provider

import (
	"context"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// StreamEvent represents a streaming response event from an LLM provider.
type StreamEvent struct {
	Type  string             // "content_delta", "tool_call", "usage", "done", "error"
	Delta *canonical.Content // For content deltas.
	Usage *canonical.Usage   // For usage updates.
	Error error              // For errors.
}

// Provider defines the interface for LLM providers.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error)
	ChatStream(ctx context.Context, req *canonical.Request) (<-chan StreamEvent, error)
}
