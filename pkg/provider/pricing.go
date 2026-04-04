package provider

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	pricingFetchTimeout = 15 * time.Second
	pricingDefaultTTL   = 24 * time.Hour
)

// ModelPricing holds per-model pricing data.
type ModelPricing struct {
	InputCost         float64 // $ per 1M input tokens
	OutputCost        float64 // $ per 1M output tokens
	CacheCost         float64 // $ per 1M cached input tokens
	ThinkingCost      float64 // $ per 1M thinking tokens (0 = use OutputCost)
	CacheCreationCost float64 // $ per 1M cache creation tokens (0 = InputCost * 1.25)
}

// EffectiveThinkingCost returns ThinkingCost or falls back to OutputCost.
func (p *ModelPricing) EffectiveThinkingCost() float64 {
	if p.ThinkingCost > 0 {
		return p.ThinkingCost
	}
	return p.OutputCost
}

// EffectiveCacheCreationCost returns CacheCreationCost or falls back to InputCost * 1.25.
func (p *ModelPricing) EffectiveCacheCreationCost() float64 {
	if p.CacheCreationCost > 0 {
		return p.CacheCreationCost
	}
	return p.InputCost * 1.25
}

// PricingResolver resolves pricing for a model by ID.
type PricingResolver interface {
	ResolvePricing(modelID string) *ModelPricing
}

// PricingStore is the DB interface the ModelRegistry depends on.
// Defined here (in provider) to avoid provider → store import.
// Implemented by the sqlite store and injected at wiring time.
type PricingStore interface {
	GetModelPricing(ctx context.Context, modelID string) (*PricingStoreResult, error)
	// BulkUpdatePricing persists pricing from OpenRouter to discovered_models.
	// Only updates rows where pricing_source is not "user".
	BulkUpdatePricing(ctx context.Context, updates []PricingUpdate) error
}

// PricingUpdate is a pricing-only update for a discovered model.
type PricingUpdate struct {
	ModelID           string  // Normalized model ID (e.g. "claude-sonnet-4-20250514")
	Provider          string  // Provider name (e.g. "anthropic")
	InputCost         float64 // $/1M input tokens
	OutputCost        float64 // $/1M output tokens
	CacheCost         float64 // $/1M cached input tokens
	ThinkingCost      float64 // $/1M thinking tokens
	CacheCreationCost float64 // $/1M cache creation tokens
	ContextWindow     int     // Max input tokens (0 = don't update)
	MaxOutputTokens   int     // Max output tokens (0 = don't update)
	Source            string  // "openrouter", "known"
}

// PricingStoreResult is the result of a pricing lookup from the store layer.
type PricingStoreResult struct {
	InputCost         float64
	OutputCost        float64
	CacheCost         float64
	ThinkingCost      float64
	CacheCreationCost float64
	ContextWindow     int
	MaxOutputTokens   int
	PricingSource     string
}

// CalculateCost computes cost and cache savings for a single request.
// Takes individual token counts (not canonical.Usage) to avoid provider → canonical import.
// When pricing is nil, returns (0, 0) — honest tracking for unknown models.
func CalculateCost(pricing *ModelPricing, inputTokens, outputTokens, cacheCreation, cacheRead, thinkingTokens int) (cost, saved float64) {
	if pricing == nil {
		return 0, 0
	}

	// Regular input = total input minus tokens read from cache.
	regularInput := inputTokens - cacheRead
	if regularInput < 0 {
		regularInput = 0
	}

	// Separate thinking tokens from regular output.
	regularOutput := outputTokens - thinkingTokens
	if regularOutput < 0 {
		regularOutput = 0
	}

	cost = float64(regularInput)/1_000_000*pricing.InputCost +
		float64(regularOutput)/1_000_000*pricing.OutputCost +
		float64(thinkingTokens)/1_000_000*pricing.EffectiveThinkingCost() +
		float64(cacheCreation)/1_000_000*pricing.EffectiveCacheCreationCost() +
		float64(cacheRead)/1_000_000*pricing.CacheCost

	// Savings: what it would have cost without caching.
	fullCost := float64(inputTokens)/1_000_000*pricing.InputCost +
		float64(regularOutput)/1_000_000*pricing.OutputCost +
		float64(thinkingTokens)/1_000_000*pricing.EffectiveThinkingCost()
	saved = fullCost - cost
	if saved < 0 {
		saved = 0
	}
	return
}

// openRouterResponse is the response from /api/v1/models.
type openRouterResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID            string             `json:"id"` // e.g. "anthropic/claude-sonnet-4"
	ContextLength int                `json:"context_length"`
	Pricing       *openRouterPricing `json:"pricing"`
	TopProvider   *openRouterTop     `json:"top_provider"`
}

type openRouterTop struct {
	MaxCompletionTokens int `json:"max_completion_tokens"`
}

type openRouterPricing struct {
	Prompt          string `json:"prompt"`            // $ per token (string)
	Completion      string `json:"completion"`        // $ per token (string)
	InputCacheRead  string `json:"input_cache_read"`  // $ per token for cached input reads
	InputCacheWrite string `json:"input_cache_write"` // $ per token for cache creation
}

// normalizeModelID strips the provider prefix from an OpenRouter model ID
// and normalizes the provider name.
func normalizeModelID(orID string) string {
	parts := strings.SplitN(orID, "/", 2)
	if len(parts) != 2 {
		return orID
	}
	return parts[1] // Return just the model part.
}

// normalizeProviderName maps OpenRouter provider names to SageClaw names.
func normalizeProviderName(orProvider string) string {
	switch orProvider {
	case "google":
		return "gemini"
	case "anthropic":
		return "anthropic"
	case "openai":
		return "openai"
	case "deepseek":
		return "deepseek"
	case "meta-llama":
		return "meta"
	case "x-ai":
		return "xai"
	default:
		return orProvider
	}
}

// matchToKnownModel tries to find a SeedPricing entry that corresponds
// to an OpenRouter model ID, using 3-tier matching:
// (a) Exact match on full ID (e.g. "anthropic/claude-sonnet-4-20250514")
// (b) Strip provider prefix, match against SeedPricing.ModelID
// (c) Fuzzy prefix match: strip -preview, -latest, date suffixes
func matchToKnownModel(orID string) string {
	parts := strings.SplitN(orID, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	orProvider := parts[0]
	orModel := parts[1]
	sageclawProvider := normalizeProviderName(orProvider)

	for _, km := range SeedPricing {
		// (a) Exact match on full ID.
		if km.ID == sageclawProvider+"/"+orModel {
			return km.ModelID
		}

		// (b) Direct model ID match.
		if km.ModelID == orModel {
			return km.ModelID
		}

		// (c) Fuzzy prefix match: strip suffixes like -preview, -latest.
		if km.Provider == sageclawProvider {
			cleaned := stripModelSuffix(orModel)
			if cleaned == km.ModelID || strings.HasPrefix(km.ModelID, cleaned) || strings.HasPrefix(cleaned, km.ModelID) {
				return km.ModelID
			}
		}
	}

	return ""
}

// stripModelSuffix removes common suffixes that differ between OpenRouter
// and direct provider model IDs.
func stripModelSuffix(modelID string) string {
	suffixes := []string{"-preview", "-latest", "-exp", "-experimental"}
	for _, s := range suffixes {
		modelID = strings.TrimSuffix(modelID, s)
	}
	// Also strip date suffixes like -20250514, -2025-01-01.
	if len(modelID) > 9 && modelID[len(modelID)-9] == '-' {
		candidate := modelID[:len(modelID)-9]
		if isDateSuffix(modelID[len(modelID)-8:]) {
			return candidate
		}
	}
	return modelID
}

func isDateSuffix(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
