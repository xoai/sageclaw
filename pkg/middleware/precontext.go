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

// PreContextSelfLearning injects self-learning rules as warnings and
// procedural knowledge as separate guidance. Corrections (gotchas,
// conventions) are framed as "Warnings from past experience."
// Procedures are framed as "Relevant procedures from past experience."
func PreContextSelfLearning(engine memory.MemoryEngine) Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPreContext {
			return next(ctx, data)
		}

		query := extractLastUserText(data.Messages)
		if query == "" {
			return next(ctx, data)
		}

		// Two separate searches: corrections at 0.6, procedures at 0.3.
		// This preserves existing correction filtering behavior while
		// allowing newer procedures (initial confidence 0.5) to surface.
		corrections, err := engine.Search(ctx, query, memory.SearchOptions{
			FilterTags:    []string{"self-learning"},
			Limit:         5,
			MinConfidence: 0.6,
		})
		if err != nil {
			corrections = nil
		}

		procedures, err := engine.Search(ctx, query, memory.SearchOptions{
			FilterTags:    []string{"self-learning", "procedure"},
			Limit:         3,
			MinConfidence: 0.3,
		})
		if err != nil {
			procedures = nil
		}

		// Filter procedures out of corrections (they appear in both searches).
		var warnings []memory.Entry
		for _, r := range corrections {
			if !hasTag(r.Tags, "procedure") {
				warnings = append(warnings, r)
			}
		}

		if len(warnings) > 0 {
			var sb strings.Builder
			sb.WriteString("Warnings from past experience:\n")
			for _, r := range warnings {
				fmt.Fprintf(&sb, "- %s\n", r.Content)
			}
			data.Injections = append(data.Injections, sb.String())
		}

		if len(procedures) > 0 {
			var sb strings.Builder
			sb.WriteString("Relevant procedures from past experience:\n")
			for _, r := range procedures {
				fmt.Fprintf(&sb, "- [%s] %s\n", r.Title, r.Content)
			}
			data.Injections = append(data.Injections, sb.String())
		}

		return next(ctx, data)
	}
}

// hasTag checks if a tag exists in a slice.
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
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
