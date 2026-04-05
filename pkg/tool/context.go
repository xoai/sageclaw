package tool

import "context"

type toolConfigKey struct{}

// WithToolConfig attaches per-agent tool configuration to the context.
func WithToolConfig(ctx context.Context, cfg map[string]map[string]any) context.Context {
	if cfg == nil {
		return ctx
	}
	return context.WithValue(ctx, toolConfigKey{}, cfg)
}

// ToolConfigFromContext retrieves per-agent tool config from the context.
func ToolConfigFromContext(ctx context.Context) map[string]map[string]any {
	if v, ok := ctx.Value(toolConfigKey{}).(map[string]map[string]any); ok {
		return v
	}
	return nil
}
