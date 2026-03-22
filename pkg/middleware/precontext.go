package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// PreContextMemory searches memory for context relevant to the current conversation.
func PreContextMemory(engine memory.MemoryEngine) Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPreContext {
			return next(ctx, data)
		}

		// Extract the last user message for search context.
		query := extractLastUserText(data.Messages)
		if query == "" {
			return next(ctx, data)
		}

		// Search for relevant memories.
		results, err := engine.Search(ctx, query, memory.SearchOptions{Limit: 3})
		if err != nil {
			// Non-fatal: log and continue.
			return next(ctx, data)
		}

		if len(results) > 0 {
			var sb strings.Builder
			sb.WriteString("Relevant context from memory:\n")
			for _, r := range results {
				fmt.Fprintf(&sb, "- [%s] %s\n", r.Title, r.Content)
			}
			data.Injections = append(data.Injections, sb.String())
		}

		return next(ctx, data)
	}
}

// PreContextSelfLearning injects self-learning rules as warnings.
func PreContextSelfLearning(engine memory.MemoryEngine) Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPreContext {
			return next(ctx, data)
		}

		query := extractLastUserText(data.Messages)
		if query == "" {
			return next(ctx, data)
		}

		// Search for self-learning rules.
		results, err := engine.Search(ctx, query, memory.SearchOptions{
			FilterTags: []string{"self-learning"},
			Limit:      3,
		})
		if err != nil {
			return next(ctx, data)
		}

		if len(results) > 0 {
			var sb strings.Builder
			sb.WriteString("Warnings from past experience:\n")
			for _, r := range results {
				fmt.Fprintf(&sb, "- %s\n", r.Content)
			}
			data.Injections = append(data.Injections, sb.String())
		}

		return next(ctx, data)
	}
}

func extractLastUserText(msgs []canonical.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			for _, c := range msgs[i].Content {
				if c.Type == "text" && c.Text != "" {
					return c.Text
				}
			}
		}
	}
	return ""
}
