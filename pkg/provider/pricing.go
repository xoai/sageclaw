package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	pricingFetchTimeout = 15 * time.Second
	pricingDefaultTTL   = 24 * time.Hour
)

// ModelPricing holds per-model pricing data.
type ModelPricing struct {
	InputCost    float64 // $ per 1M input tokens
	OutputCost   float64 // $ per 1M output tokens
	CacheCost    float64 // $ per 1M cached input tokens
	ThinkingCost float64 // $ per 1M thinking tokens (0 = use OutputCost)
}

// EffectiveThinkingCost returns ThinkingCost or falls back to OutputCost.
func (p *ModelPricing) EffectiveThinkingCost() float64 {
	if p.ThinkingCost > 0 {
		return p.ThinkingCost
	}
	return p.OutputCost
}

// PricingCache holds live pricing data fetched from OpenRouter.
// Thread-safe. Falls back to KnownModels when cache is empty or stale.
type PricingCache struct {
	mu      sync.RWMutex
	models  map[string]*ModelPricing // key: model ID (e.g. "claude-sonnet-4-20250514")
	fetched time.Time
	ttl     time.Duration
}

// NewPricingCache creates a new empty pricing cache.
func NewPricingCache() *PricingCache {
	return &PricingCache{
		models: make(map[string]*ModelPricing),
		ttl:    pricingDefaultTTL,
	}
}

// FindModelPricing resolves pricing for a model.
// Resolution order: PricingCache → KnownModels → nil.
func (pc *PricingCache) FindModelPricing(modelID string) *ModelPricing {
	if pc != nil {
		pc.mu.RLock()
		if p, ok := pc.models[modelID]; ok {
			pc.mu.RUnlock()
			return p
		}
		pc.mu.RUnlock()
	}

	// Fallback to KnownModels.
	m := FindModel(modelID)
	if m != nil {
		return &ModelPricing{
			InputCost:    m.InputCost,
			OutputCost:   m.OutputCost,
			CacheCost:    m.CacheCost,
			ThinkingCost: m.ThinkingCost,
		}
	}
	return nil
}

// IsFresh returns true if the cache has been populated within the TTL.
func (pc *PricingCache) IsFresh() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return !pc.fetched.IsZero() && time.Since(pc.fetched) < pc.ttl
}

// Count returns the number of cached pricing entries.
func (pc *PricingCache) Count() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return len(pc.models)
}

// StartPricingRefresh starts a background goroutine that fetches pricing
// from OpenRouter on startup and every 24 hours. Fail-open: on error,
// logs a warning and keeps stale data.
func (pc *PricingCache) StartPricingRefresh(ctx context.Context) {
	go func() {
		// Initial fetch.
		if err := pc.fetchFromOpenRouter(ctx); err != nil {
			log.Printf("pricing: initial fetch failed: %v (using local pricing)", err)
		} else {
			log.Printf("pricing: loaded %d models from OpenRouter", pc.Count())
		}

		ticker := time.NewTicker(pc.ttl)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := pc.fetchFromOpenRouter(ctx); err != nil {
					log.Printf("pricing: refresh failed: %v (keeping stale data)", err)
				} else {
					log.Printf("pricing: refreshed %d models from OpenRouter", pc.Count())
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// fetchFromOpenRouter fetches the model list from OpenRouter and parses pricing.
func (pc *PricingCache) fetchFromOpenRouter(ctx context.Context) error {
	fetchCtx, cancel := context.WithTimeout(ctx, pricingFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, "GET", openRouterModelsURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	return pc.parseOpenRouterResponse(body)
}

// openRouterResponse is the response from /api/v1/models.
type openRouterResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID      string               `json:"id"`      // e.g. "anthropic/claude-sonnet-4"
	Pricing *openRouterPricing   `json:"pricing"`
}

type openRouterPricing struct {
	Prompt     string `json:"prompt"`     // $ per token (string)
	Completion string `json:"completion"` // $ per token (string)
}

// parseOpenRouterResponse parses OpenRouter's model list and updates the cache.
func (pc *PricingCache) parseOpenRouterResponse(body []byte) error {
	var resp openRouterResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	newModels := make(map[string]*ModelPricing)

	for _, m := range resp.Data {
		if m.Pricing == nil {
			continue
		}

		inputPerToken := parseFloat(m.Pricing.Prompt)
		outputPerToken := parseFloat(m.Pricing.Completion)
		if inputPerToken == 0 && outputPerToken == 0 {
			continue // Free or unparseable — skip.
		}

		// Convert $/token to $/1M tokens.
		pricing := &ModelPricing{
			InputCost:  inputPerToken * 1_000_000,
			OutputCost: outputPerToken * 1_000_000,
			CacheCost:  inputPerToken * 1_000_000 * 0.1, // Estimate: cached = 10% of input.
		}

		// Store under the normalized model ID.
		normalizedID := normalizeModelID(m.ID)
		if normalizedID != "" {
			newModels[normalizedID] = pricing
		}

		// Also try to match against KnownModels for local ID mapping.
		if matched := matchToKnownModel(m.ID); matched != "" {
			newModels[matched] = pricing
		}
	}

	pc.mu.Lock()
	pc.models = newModels
	pc.fetched = time.Now()
	pc.mu.Unlock()

	return nil
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

// matchToKnownModel tries to find a KnownModels entry that corresponds
// to an OpenRouter model ID, using 3-tier matching:
// (a) Exact match on full ID (e.g. "anthropic/claude-sonnet-4-20250514")
// (b) Strip provider prefix, match against KnownModels.ModelID
// (c) Fuzzy prefix match: strip -preview, -latest, date suffixes
func matchToKnownModel(orID string) string {
	parts := strings.SplitN(orID, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	orProvider := parts[0]
	orModel := parts[1]
	sageclawProvider := normalizeProviderName(orProvider)

	for _, km := range KnownModels {
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
