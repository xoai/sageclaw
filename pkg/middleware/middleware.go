package middleware

import (
	"context"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// HookPoint identifies where in the agent loop a middleware runs.
type HookPoint string

const (
	HookPreContext  HookPoint = "pre_context"
	HookPostTool   HookPoint = "post_tool"
	HookPreResponse HookPoint = "pre_response"
)

// HookData carries context for middleware execution.
type HookData struct {
	HookPoint  HookPoint
	Request    *canonical.Request    // The current request being built.
	Messages   []canonical.Message   // Conversation history.
	ToolCall   *canonical.ToolCall   // PostTool only: the tool that was called.
	ToolResult *canonical.ToolResult // PostTool only: the result.
	Response   *canonical.Response   // PreResponse only: the full agent response.
	Injections []string              // PreContext: additional system instructions.
	Metadata   map[string]any        // Arbitrary middleware state.
}

// NextFunc calls the next middleware in the chain.
type NextFunc func(ctx context.Context, data *HookData) error

// Middleware is a function that processes hook data and calls next.
type Middleware func(ctx context.Context, data *HookData, next NextFunc) error

// Chain composes multiple middlewares into a single middleware.
func Chain(middlewares ...Middleware) Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		// Build the chain from the end.
		chain := next
		for i := len(middlewares) - 1; i >= 0; i-- {
			mw := middlewares[i]
			current := chain
			chain = func(ctx context.Context, data *HookData) error {
				return mw(ctx, data, current)
			}
		}
		return chain(ctx, data)
	}
}
