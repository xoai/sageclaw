package context

import (
	"context"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

// LLMCaller makes a simple text-in/text-out LLM call for utility purposes
// (micro-compact, summaries). Returns the text response or an error.
type LLMCaller func(ctx context.Context, systemPrompt string, userContent string, timeout time.Duration) (string, error)

// NewLLMCaller creates an LLMCaller that resolves the fast-tier model from
// the router. Resolution order: combo tail → TierFast → main model fallback.
func NewLLMCaller(router *provider.Router, mainModel string) LLMCaller {
	if router == nil {
		return nil
	}

	return func(ctx context.Context, systemPrompt string, userContent string, timeout time.Duration) (string, error) {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Resolve provider + model: prefer fast tier.
		prov, model := resolveFastProvider(router, mainModel)
		if prov == nil {
			return "", fmt.Errorf("no provider available for utility call")
		}

		req := &canonical.Request{
			Model:     model,
			System:    systemPrompt,
			MaxTokens: 1024,
			Messages: []canonical.Message{
				{Role: "user", Content: []canonical.Content{{Type: "text", Text: userContent}}},
			},
		}

		resp, err := prov.Chat(callCtx, req)
		if err != nil {
			return "", fmt.Errorf("utility LLM call: %w", err)
		}

		// Extract text from response messages.
		for _, msg := range resp.Messages {
			for _, c := range msg.Content {
				if c.Text != "" {
					return c.Text, nil
				}
			}
		}
		return "", fmt.Errorf("utility LLM call: empty response")
	}
}

// resolveFastProvider resolves the cheapest available provider+model.
// Priority: combo tail → TierFast → main model.
func resolveFastProvider(router *provider.Router, mainModel string) (provider.Provider, string) {
	// If main model is a combo, use the tail (cheapest).
	if provider.IsCombo(mainModel) {
		if p, m, err := router.ComboTail(provider.ComboName(mainModel)); err == nil && p != nil {
			return p, m
		}
	}

	// Try fast tier.
	if router.HasTier(provider.TierFast) {
		if p, m := router.Resolve(provider.TierFast); p != nil {
			return p, m
		}
	}

	// Fallback to main model via router.
	if p, m := router.Resolve(provider.TierStrong); p != nil {
		return p, m
	}

	return nil, ""
}
