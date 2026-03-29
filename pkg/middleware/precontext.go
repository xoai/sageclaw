package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// DefaultMaxInjectionTokens is the default cap for memory injection per LLM call.
// Prevents memory from consuming excessive context in tight budget situations.
// Source: DeerFlow config/memory_config.py — max_injection_tokens: 2000.
const DefaultMaxInjectionTokens = 2000

// PreContextMemory searches memory for context relevant to the current conversation.
// Filters by MinConfidence 0.6 and caps injection at MaxInjectionTokens.
func PreContextMemory(engine memory.MemoryEngine) Middleware {
	return PreContextMemoryWithConfig(engine, 0.6, DefaultMaxInjectionTokens)
}

// PreContextMemoryWithConfig creates a memory middleware with explicit settings.
func PreContextMemoryWithConfig(engine memory.MemoryEngine, minConfidence float64, maxInjectionTokens int) Middleware {
	if maxInjectionTokens <= 0 {
		maxInjectionTokens = DefaultMaxInjectionTokens
	}

	return func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPreContext {
			return next(ctx, data)
		}

		// Extract the last user message for search context.
		query := extractLastUserText(data.Messages)
		if query == "" {
			return next(ctx, data)
		}

		// Search for relevant memories with confidence filter.
		results, err := engine.Search(ctx, query, memory.SearchOptions{
			Limit:         10, // Fetch more, cap by tokens below.
			MinConfidence: minConfidence,
		})
		if err != nil {
			// Non-fatal: continue without memory.
			return next(ctx, data)
		}

		if len(results) > 0 {
			var sb strings.Builder
			sb.WriteString("Relevant context from memory:\n")
			tokensUsed := 0
			for _, r := range results {
				line := fmt.Sprintf("- [%s] %s\n", r.Title, r.Content)
				lineTokens := len(line) / 4 // chars/4 estimate
				if tokensUsed+lineTokens > maxInjectionTokens {
					break // Budget exhausted.
				}
				sb.WriteString(line)
				tokensUsed += lineTokens
			}
			if tokensUsed > 0 {
				data.Injections = append(data.Injections, sb.String())
			}
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
