package middleware

import (
	"context"
	"log"
)

// PreResponseLog logs usage statistics before response delivery.
func PreResponseLog() Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPreResponse {
			return next(ctx, data)
		}

		if data.Response != nil {
			log.Printf("[usage] input=%d output=%d cache_read=%d cache_create=%d",
				data.Response.Usage.InputTokens,
				data.Response.Usage.OutputTokens,
				data.Response.Usage.CacheRead,
				data.Response.Usage.CacheCreation,
			)
		}

		return next(ctx, data)
	}
}
