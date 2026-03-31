package provider

import (
	"sync"
	"sync/atomic"
)

// CacheStats tracks prompt caching hit rates and cost savings.
type CacheStats struct {
	TotalRequests   atomic.Int64 `json:"total_requests"`
	CacheHits       atomic.Int64 `json:"cache_hits"`       // Requests where cache_read > 0
	TotalInput      atomic.Int64 `json:"total_input"`       // Total input tokens
	TotalOutput     atomic.Int64 `json:"total_output"`      // Total output tokens
	TotalThinking   atomic.Int64 `json:"total_thinking"`    // Total thinking/reasoning tokens
	CacheCreation   atomic.Int64 `json:"cache_creation"`    // Tokens written to cache
	CacheRead       atomic.Int64 `json:"cache_read"`        // Tokens read from cache
	mu              sync.RWMutex
}

// Global cache stats instance.
var GlobalCacheStats = &CacheStats{}

// Record adds a request's usage to the stats.
func (cs *CacheStats) Record(inputTokens, outputTokens, cacheCreation, cacheRead, thinkingTokens int) {
	cs.TotalRequests.Add(1)
	cs.TotalInput.Add(int64(inputTokens))
	cs.TotalOutput.Add(int64(outputTokens))
	cs.TotalThinking.Add(int64(thinkingTokens))
	cs.CacheCreation.Add(int64(cacheCreation))
	cs.CacheRead.Add(int64(cacheRead))
	if cacheRead > 0 {
		cs.CacheHits.Add(1)
	}
}

// Snapshot returns a point-in-time copy of the stats.
func (cs *CacheStats) Snapshot() CacheStatsSnapshot {
	return CacheStatsSnapshot{
		TotalRequests: cs.TotalRequests.Load(),
		CacheHits:     cs.CacheHits.Load(),
		TotalInput:    cs.TotalInput.Load(),
		TotalOutput:   cs.TotalOutput.Load(),
		TotalThinking: cs.TotalThinking.Load(),
		CacheCreation: cs.CacheCreation.Load(),
		CacheRead:     cs.CacheRead.Load(),
	}
}

// CacheStatsSnapshot is a serializable point-in-time copy.
type CacheStatsSnapshot struct {
	TotalRequests int64   `json:"total_requests"`
	CacheHits     int64   `json:"cache_hits"`
	HitRate       float64 `json:"hit_rate"`
	TotalInput    int64   `json:"total_input"`
	TotalOutput   int64   `json:"total_output"`
	TotalThinking int64   `json:"total_thinking"`
	CacheCreation int64   `json:"cache_creation"`
	CacheRead     int64   `json:"cache_read"`
	CostSavings   float64 `json:"cost_savings_pct"`  // Estimated % savings from caching
	EstCostUSD    float64 `json:"est_cost_usd"`      // Estimated total cost in USD
	EstSavedUSD   float64 `json:"est_saved_usd"`     // Estimated savings from caching in USD
}

func (s CacheStatsSnapshot) WithCalculations() CacheStatsSnapshot {
	if s.TotalRequests > 0 {
		s.HitRate = float64(s.CacheHits) / float64(s.TotalRequests) * 100
	}
	// Cache reads cost 10% of regular input tokens (Anthropic pricing).
	if s.TotalInput > 0 {
		savedTokens := float64(s.CacheRead) * 0.9
		s.CostSavings = savedTokens / float64(s.TotalInput+s.CacheRead) * 100
	}
	// Estimate cost using Claude Sonnet pricing as baseline ($3/$15 per 1M).
	s.EstCostUSD = float64(s.TotalInput)/1_000_000*3.0 + float64(s.TotalOutput)/1_000_000*15.0
	// Savings from cache reads (cached input costs $0.30 instead of $3.00).
	s.EstSavedUSD = float64(s.CacheRead) / 1_000_000 * 2.7
	return s
}

// BudgetConfig holds cost budget settings.
type BudgetConfig struct {
	DailyLimitUSD   float64 `json:"daily_limit_usd" yaml:"daily_limit_usd"`     // 0 = no limit
	MonthlyLimitUSD float64 `json:"monthly_limit_usd" yaml:"monthly_limit_usd"` // 0 = no limit
	AlertAtPercent  float64 `json:"alert_at_percent" yaml:"alert_at_percent"`    // Alert when budget reaches this %. Default: 80
}

// DefaultBudgetConfig returns sensible defaults (no limits).
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		DailyLimitUSD:   0,
		MonthlyLimitUSD: 0,
		AlertAtPercent:  80,
	}
}
