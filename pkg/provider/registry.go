package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// ModelRegistry is the single source of truth for model pricing.
// Thread-safe. Backed by PricingStore with in-memory cache for hot-path lookups.
// Owns the OpenRouter pricing refresh goroutine.
type ModelRegistry struct {
	store PricingStore
	mu    sync.RWMutex
	cache map[string]*ModelPricing // in-memory cache, refreshed on invalidation
}

// NewModelRegistry creates a registry backed by the given store.
func NewModelRegistry(store PricingStore) *ModelRegistry {
	return &ModelRegistry{
		store: store,
		cache: make(map[string]*ModelPricing),
	}
}

// ResolvePricing returns pricing for a model. Returns nil for unknown models (cost = $0).
// Uses in-memory cache for hot-path performance; falls through to DB on cache miss.
func (r *ModelRegistry) ResolvePricing(modelID string) *ModelPricing {
	// Fast path: check cache.
	r.mu.RLock()
	if p, ok := r.cache[modelID]; ok {
		r.mu.RUnlock()
		return p
	}
	r.mu.RUnlock()

	// Slow path: query store.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.store.GetModelPricing(ctx, modelID)
	if err != nil {
		log.Printf("pricing: store lookup failed for %q: %v", modelID, err)
		return nil
	}
	if result == nil {
		// Don't cache misses — the model may appear after discovery or
		// OpenRouter refresh. The DB query is cheap for cache misses.
		return nil
	}

	pricing := &ModelPricing{
		InputCost:         result.InputCost,
		OutputCost:        result.OutputCost,
		CacheCost:         result.CacheCost,
		ThinkingCost:      result.ThinkingCost,
		CacheCreationCost: result.CacheCreationCost,
	}

	r.mu.Lock()
	r.cache[modelID] = pricing
	r.mu.Unlock()

	return pricing
}

// ResolveContextWindow returns the context window for a model.
// Falls back to 200000 (Claude default) if unknown.
func (r *ModelRegistry) ResolveContextWindow(modelID string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.store.GetModelPricing(ctx, modelID)
	if err != nil || result == nil || result.ContextWindow <= 0 {
		return 200000 // Default fallback.
	}
	return result.ContextWindow
}

// InvalidateCache clears the in-memory cache, forcing fresh DB lookups.
// Call after model discovery, pricing overrides, or OpenRouter refresh.
func (r *ModelRegistry) InvalidateCache() {
	r.mu.Lock()
	r.cache = make(map[string]*ModelPricing)
	r.mu.Unlock()
}

// SeedFromKnownModels inserts SeedPricing into the store as baseline data.
// Uses pricing_source="known" which gets overwritten by OpenRouter refresh.
// Safe to call on every startup — the ON CONFLICT clause in BulkUpdatePricing
// won't overwrite "openrouter" or "user" rows with "known" data.
func (r *ModelRegistry) SeedFromKnownModels(ctx context.Context) {
	var updates []PricingUpdate
	for _, m := range SeedPricing {
		if m.InputCost == 0 && m.OutputCost == 0 {
			continue // Skip free models (ollama, github).
		}
		updates = append(updates, PricingUpdate{
			ModelID:       m.ModelID,
			Provider:      m.Provider,
			InputCost:     m.InputCost,
			OutputCost:    m.OutputCost,
			CacheCost:     m.CacheCost,
			ThinkingCost:  m.ThinkingCost,
			Source:        "known",
			ContextWindow: m.ContextWindow,
		})
	}
	if len(updates) == 0 {
		return
	}
	if err := r.store.BulkUpdatePricing(ctx, updates); err != nil {
		log.Printf("pricing: seed from KnownModels failed: %v", err)
	} else {
		log.Printf("pricing: seeded %d models from KnownModels", len(updates))
	}
}

// StartPricingRefresh starts a background goroutine that fetches pricing
// from OpenRouter on startup and every 24 hours. After each fetch, pricing
// is persisted to discovered_models via the store and the in-memory cache
// is invalidated. Fail-open: on error, logs and keeps stale data.
func (r *ModelRegistry) StartPricingRefresh(ctx context.Context) {
	go func() {
		if n, err := r.fetchAndPersist(ctx); err != nil {
			log.Printf("pricing: initial OpenRouter fetch failed: %v (using local pricing)", err)
		} else {
			log.Printf("pricing: loaded %d models from OpenRouter", n)
		}

		ticker := time.NewTicker(pricingDefaultTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n, err := r.fetchAndPersist(ctx); err != nil {
					log.Printf("pricing: OpenRouter refresh failed: %v (keeping stale data)", err)
				} else {
					log.Printf("pricing: refreshed %d models from OpenRouter", n)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// fetchAndPersist fetches pricing from OpenRouter, persists to DB, and
// invalidates the in-memory cache. Returns the number of models updated.
func (r *ModelRegistry) fetchAndPersist(ctx context.Context) (int, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, pricingFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, "GET", openRouterModelsURL, nil)
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB cap
	if err != nil {
		return 0, fmt.Errorf("reading body: %w", err)
	}

	updates, err := parseOpenRouterPricing(body)
	if err != nil {
		return 0, err
	}

	// Persist to DB (only updates non-user rows).
	if err := r.store.BulkUpdatePricing(ctx, updates); err != nil {
		return 0, fmt.Errorf("persisting pricing: %w", err)
	}

	// Invalidate cache so next ResolvePricing reads fresh data from DB.
	r.InvalidateCache()

	return len(updates), nil
}

// parseOpenRouterPricing parses OpenRouter's model list into PricingUpdates.
func parseOpenRouterPricing(body []byte) ([]PricingUpdate, error) {
	var resp openRouterResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var updates []PricingUpdate
	seen := make(map[string]bool)

	for _, m := range resp.Data {
		if m.Pricing == nil {
			continue
		}

		inputPerToken := parseFloat(m.Pricing.Prompt)
		outputPerToken := parseFloat(m.Pricing.Completion)
		if inputPerToken == 0 && outputPerToken == 0 {
			continue
		}

		inputCost := inputPerToken * 1_000_000
		outputCost := outputPerToken * 1_000_000

		// Use real cache pricing from OpenRouter when available.
		// Falls back to 10% of input cost only if API doesn't provide it.
		cacheCost := inputCost * 0.1
		if m.Pricing.InputCacheRead != "" {
			if parsed := parseFloat(m.Pricing.InputCacheRead); parsed > 0 {
				cacheCost = parsed * 1_000_000
			}
		}

		var cacheCreationCost float64
		if m.Pricing.InputCacheWrite != "" {
			if parsed := parseFloat(m.Pricing.InputCacheWrite); parsed > 0 {
				cacheCreationCost = parsed * 1_000_000
			}
		}

		// Extract context window and max output tokens.
		contextWindow := m.ContextLength
		var maxOutputTokens int
		if m.TopProvider != nil {
			maxOutputTokens = m.TopProvider.MaxCompletionTokens
		}

		providerName := ""
		normalizedID := normalizeModelID(m.ID)
		if normalizedID != "" && !seen[normalizedID] {
			if parts := splitModelID(m.ID); len(parts) == 2 {
				providerName = normalizeProviderName(parts[0])
			}
			updates = append(updates, PricingUpdate{
				ModelID:           normalizedID,
				Provider:          providerName,
				InputCost:         inputCost,
				OutputCost:        outputCost,
				CacheCost:         cacheCost,
				CacheCreationCost: cacheCreationCost,
				ContextWindow:     contextWindow,
				MaxOutputTokens:   maxOutputTokens,
				Source:            "openrouter",
			})
			seen[normalizedID] = true
		}

		// Also emit under the KnownModels ID if it differs.
		if matched := matchToKnownModel(m.ID); matched != "" && !seen[matched] {
			updates = append(updates, PricingUpdate{
				ModelID:           matched,
				Provider:          providerName,
				InputCost:         inputCost,
				OutputCost:        outputCost,
				CacheCost:         cacheCost,
				CacheCreationCost: cacheCreationCost,
				ContextWindow:     contextWindow,
				MaxOutputTokens:   maxOutputTokens,
				Source:            "openrouter",
			})
			seen[matched] = true
		}
	}

	return updates, nil
}

// splitModelID splits "provider/model" into parts.
func splitModelID(id string) []string {
	for i, c := range id {
		if c == '/' {
			return []string{id[:i], id[i+1:]}
		}
	}
	return []string{id}
}
