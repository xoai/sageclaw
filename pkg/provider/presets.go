package provider

import (
	"sort"
)

// DiscoveredModelInfo is the subset of discovered model data needed for preset generation.
type DiscoveredModelInfo struct {
	ModelID       string
	Provider      string
	OutputCost    float64
	ContextWindow int
}

// GeneratePresetCombos builds four preset combos (strong, fast, balanced, local)
// from discovered models, filtered to only include connected providers.
//
// Selection logic:
//   - strong: cloud models sorted by output_cost DESC, top 3
//   - fast: cloud models with context >= 100K, sorted by output_cost ASC, top 3
//   - balanced: cloud models excluding top/bottom 20% by cost, sorted by context DESC, top 3
//   - local: ollama models only, sorted by context DESC
func GeneratePresetCombos(models []DiscoveredModelInfo, connectedProviders []string) map[string]Combo {
	// nil connectedProviders = no filter (include all providers).
	var connected map[string]bool
	if connectedProviders != nil {
		connected = make(map[string]bool, len(connectedProviders))
		for _, p := range connectedProviders {
			connected[p] = true
		}
	}

	// Split into cloud vs local, optionally filtering to connected providers.
	var cloud, local []DiscoveredModelInfo
	for _, m := range models {
		if connected != nil && !connected[m.Provider] {
			continue
		}
		if m.Provider == "ollama" {
			local = append(local, m)
		} else {
			cloud = append(cloud, m)
		}
	}

	presets := make(map[string]Combo)

	// strong: most expensive cloud models (providers price by capability).
	if combo := buildStrong(cloud); len(combo.Models) > 0 {
		presets["strong"] = combo
	}

	// fast: cheapest cloud models with large context.
	if combo := buildFast(cloud); len(combo.Models) > 0 {
		presets["fast"] = combo
	}

	// balanced: mid-tier by cost, sorted by context window.
	if combo := buildBalanced(cloud); len(combo.Models) > 0 {
		presets["balanced"] = combo
	}

	// local: ollama models by context window.
	if combo := buildLocal(local); len(combo.Models) > 0 {
		presets["local"] = combo
	}

	return presets
}

func buildStrong(cloud []DiscoveredModelInfo) Combo {
	sorted := make([]DiscoveredModelInfo, len(cloud))
	copy(sorted, cloud)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].OutputCost != sorted[j].OutputCost {
			return sorted[i].OutputCost > sorted[j].OutputCost
		}
		return sorted[i].ModelID < sorted[j].ModelID // Stable tie-break.
	})
	return Combo{
		Name:     "strong",
		Strategy: "priority",
		Models:   toComboModels(sorted, 3),
	}
}

func buildFast(cloud []DiscoveredModelInfo) Combo {
	var filtered []DiscoveredModelInfo
	for _, m := range cloud {
		if m.ContextWindow >= 100000 {
			filtered = append(filtered, m)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].OutputCost != filtered[j].OutputCost {
			return filtered[i].OutputCost < filtered[j].OutputCost
		}
		return filtered[i].ModelID < filtered[j].ModelID
	})
	return Combo{
		Name:     "fast",
		Strategy: "priority",
		Models:   toComboModels(filtered, 3),
	}
}

func buildBalanced(cloud []DiscoveredModelInfo) Combo {
	if len(cloud) < 3 {
		sorted := make([]DiscoveredModelInfo, len(cloud))
		copy(sorted, cloud)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].ContextWindow != sorted[j].ContextWindow {
				return sorted[i].ContextWindow > sorted[j].ContextWindow
			}
			return sorted[i].ModelID < sorted[j].ModelID
		})
		return Combo{
			Name:     "balanced",
			Strategy: "priority",
			Models:   toComboModels(sorted, 3),
		}
	}

	// Sort by cost to find percentile boundaries.
	byCost := make([]DiscoveredModelInfo, len(cloud))
	copy(byCost, cloud)
	sort.Slice(byCost, func(i, j int) bool {
		if byCost[i].OutputCost != byCost[j].OutputCost {
			return byCost[i].OutputCost < byCost[j].OutputCost
		}
		return byCost[i].ModelID < byCost[j].ModelID
	})

	// Exclude top and bottom 20%.
	n := len(byCost)
	lo := n / 5
	hi := n - n/5
	if lo >= hi {
		lo = 0
		hi = n
	}

	// Copy mid-tier slice to avoid mutating byCost's backing array.
	mid := make([]DiscoveredModelInfo, hi-lo)
	copy(mid, byCost[lo:hi])

	sort.Slice(mid, func(i, j int) bool {
		if mid[i].ContextWindow != mid[j].ContextWindow {
			return mid[i].ContextWindow > mid[j].ContextWindow
		}
		return mid[i].ModelID < mid[j].ModelID
	})

	return Combo{
		Name:     "balanced",
		Strategy: "priority",
		Models:   toComboModels(mid, 3),
	}
}

func buildLocal(local []DiscoveredModelInfo) Combo {
	sorted := make([]DiscoveredModelInfo, len(local))
	copy(sorted, local)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ContextWindow != sorted[j].ContextWindow {
			return sorted[i].ContextWindow > sorted[j].ContextWindow
		}
		return sorted[i].ModelID < sorted[j].ModelID
	})
	// No per-provider dedup for local — all ollama models are fallbacks.
	models := make([]ComboModel, 0, len(sorted))
	for _, m := range sorted {
		models = append(models, ComboModel{Provider: m.Provider, ModelID: m.ModelID})
	}
	return Combo{
		Name:     "local",
		Strategy: "priority",
		Models:   models,
	}
}

// toComboModels converts DiscoveredModelInfo to ComboModel, limited to max entries (0 = no limit).
// Deduplicates by provider to ensure diversity across providers in the fallback chain.
func toComboModels(models []DiscoveredModelInfo, max int) []ComboModel {
	seen := make(map[string]bool)
	var result []ComboModel
	for _, m := range models {
		if seen[m.Provider] {
			continue // One model per provider for diversity.
		}
		seen[m.Provider] = true
		result = append(result, ComboModel{
			Provider: m.Provider,
			ModelID:  m.ModelID,
		})
		if max > 0 && len(result) >= max {
			break
		}
	}
	return result
}
