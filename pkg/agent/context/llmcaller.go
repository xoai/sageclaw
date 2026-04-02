package context

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

// LLMCaller makes a simple text-in/text-out LLM call for utility purposes
// (micro-compact, summaries). Returns the text response or an error.
type LLMCaller func(ctx context.Context, systemPrompt string, userContent string, timeout time.Duration) (string, error)

// NewLLMCaller creates an LLMCaller that resolves the fast-tier model from
// the router. If utilityOverride is set (non-empty, not "auto"), it uses that
// model directly. Resolution order for auto: combo tail → TierFast → main model.
func NewLLMCaller(router *provider.Router, mainModel string, utilityOverride string) LLMCaller {
	if router == nil {
		return nil
	}

	return func(ctx context.Context, systemPrompt string, userContent string, timeout time.Duration) (string, error) {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Resolve provider + model.
		var prov provider.Provider
		var model string

		if utilityOverride != "" && utilityOverride != "auto" {
			prov, model = resolveOverrideModel(router, utilityOverride)
			if prov == nil {
				log.Printf("[context-pipeline] utility model override %q not available, falling back to auto", utilityOverride)
				prov, model = resolveFastProvider(router, mainModel)
			}
		} else {
			prov, model = resolveFastProvider(router, mainModel)
		}

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

// resolveOverrideModel finds the provider for a specific model ID.
func resolveOverrideModel(router *provider.Router, modelID string) (provider.Provider, string) {
	// Look up in known models to find the provider name.
	if info := provider.FindModel(modelID); info != nil {
		if p, ok := router.GetProvider(info.Provider); ok {
			return p, modelID
		}
	}

	// For unknown models (e.g., Ollama custom), try the ollama provider.
	if p, ok := router.GetProvider("ollama"); ok {
		return p, modelID
	}

	return nil, ""
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
