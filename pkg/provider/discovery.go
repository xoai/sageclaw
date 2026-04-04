package provider

import (
	"context"
	"log"
	"time"
)

// DiscoveredModelResult holds the results of a single provider's model discovery.
type DiscoveredModelResult struct {
	Provider string
	Models   []ModelInfo
	Err      error
}

// DiscoverAll runs ListModels() on all registered providers that implement ModelLister.
// Returns results per provider. Each provider gets a 10s timeout.
// Errors are per-provider — one failing provider doesn't block others.
func DiscoverAll(ctx context.Context, providers map[string]Provider) []DiscoveredModelResult {
	var results []DiscoveredModelResult

	for name, p := range providers {
		lister, ok := p.(ModelLister)
		if !ok {
			continue
		}

		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		models, err := lister.ListModels(callCtx)
		cancel()

		if err != nil {
			log.Printf("discover models: %s: %v", name, err)
			results = append(results, DiscoveredModelResult{Provider: name, Err: err})
			continue
		}

		log.Printf("discover models: %s: found %d models", name, len(models))
		results = append(results, DiscoveredModelResult{Provider: name, Models: models})
	}

	return results
}

// TotalDiscovered returns the total number of models across all successful results.
func TotalDiscovered(results []DiscoveredModelResult) int {
	total := 0
	for _, r := range results {
		total += len(r.Models)
	}
	return total
}
