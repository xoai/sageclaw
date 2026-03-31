package provider

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// BudgetEngine tracks per-request costs and enforces spending limits.
type BudgetEngine struct {
	db           *sql.DB
	mu           sync.RWMutex
	config       BudgetConfig
	alerts       []BudgetAlert
	pricingCache *PricingCache // Optional: live pricing from OpenRouter.
}

// BudgetAlert represents a triggered budget alert.
type BudgetAlert struct {
	ID           int     `json:"id"`
	AlertType    string  `json:"alert_type"`    // "warning" or "limit_reached"
	Period       string  `json:"period"`        // "daily" or "monthly"
	LimitUSD     float64 `json:"limit_usd"`
	SpentUSD     float64 `json:"spent_usd"`
	Percent      float64 `json:"percent"`
	Acknowledged bool    `json:"acknowledged"`
	CreatedAt    string  `json:"created_at"`
}

// SpendingSummary is a snapshot of current spending.
type SpendingSummary struct {
	TodayUSD         float64 `json:"today_usd"`
	TodayRequests    int     `json:"today_requests"`
	TodaySavedUSD    float64 `json:"today_saved_usd"`
	MonthUSD         float64 `json:"month_usd"`
	MonthRequests    int     `json:"month_requests"`
	MonthSavedUSD    float64 `json:"month_saved_usd"`
	DailyLimitUSD    float64 `json:"daily_limit_usd"`
	MonthlyLimitUSD  float64 `json:"monthly_limit_usd"`
	DailyPercent     float64 `json:"daily_percent"`
	MonthlyPercent   float64 `json:"monthly_percent"`
	DailyRemaining   float64 `json:"daily_remaining"`
	MonthlyRemaining float64 `json:"monthly_remaining"`
	AlertAtPercent   float64 `json:"alert_at_percent"`
	HardStop         bool    `json:"hard_stop"`
}

// CostEntry represents a single request's cost.
type CostEntry struct {
	SessionID      string  `json:"session_id"`
	AgentID        string  `json:"agent_id"`
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	InputTokens    int     `json:"input_tokens"`
	OutputTokens   int     `json:"output_tokens"`
	CacheCreation  int     `json:"cache_creation"`
	CacheRead      int     `json:"cache_read"`
	ThinkingTokens int     `json:"thinking_tokens"`
	CostUSD        float64 `json:"cost_usd"`
	SavedUSD       float64 `json:"saved_usd"`
}

// DailyCost is cost per day for charts.
type DailyCost struct {
	Date     string  `json:"date"`
	CostUSD  float64 `json:"cost_usd"`
	SavedUSD float64 `json:"saved_usd"`
	Requests int     `json:"requests"`
}

// NewBudgetEngine creates a budget engine backed by the given DB.
// pricingCache is optional — pass nil to use only KnownModels for pricing.
func NewBudgetEngine(db *sql.DB, pricingCache ...*PricingCache) *BudgetEngine {
	be := &BudgetEngine{db: db}
	if len(pricingCache) > 0 {
		be.pricingCache = pricingCache[0]
	}
	be.loadConfig()
	return be
}

func (be *BudgetEngine) loadConfig() {
	be.mu.Lock()
	defer be.mu.Unlock()

	row := be.db.QueryRow(`SELECT daily_limit_usd, monthly_limit_usd, alert_at_percent, hard_stop FROM budgets WHERE id = 'default'`)
	var hardStop int
	if err := row.Scan(&be.config.DailyLimitUSD, &be.config.MonthlyLimitUSD, &be.config.AlertAtPercent, &hardStop); err != nil {
		be.config = DefaultBudgetConfig()
	}
}

// RecordCost logs a request's cost and checks budget limits.
// Returns an error if hard_stop is enabled and the budget is exceeded.
func (be *BudgetEngine) RecordCost(ctx context.Context, entry CostEntry) error {
	// Calculate cost from model pricing if not provided.
	if entry.CostUSD == 0 {
		entry.CostUSD, entry.SavedUSD = be.calculateCost(entry)
	}

	// Insert cost log.
	_, err := be.db.ExecContext(ctx,
		`INSERT INTO cost_log (session_id, agent_id, provider, model, input_tokens, output_tokens, cache_creation, cache_read, thinking_tokens, cost_usd, saved_usd)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.SessionID, entry.AgentID, entry.Provider, entry.Model,
		entry.InputTokens, entry.OutputTokens, entry.CacheCreation, entry.CacheRead,
		entry.ThinkingTokens, entry.CostUSD, entry.SavedUSD)
	if err != nil {
		return fmt.Errorf("recording cost: %w", err)
	}

	// Check limits.
	return be.checkLimits(ctx)
}

func (be *BudgetEngine) calculateCost(entry CostEntry) (cost, saved float64) {
	// Try live pricing first, then KnownModels, then Sonnet fallback.
	pricing := be.resolvePricing(entry.Model)

	// Regular input cost.
	regularInput := entry.InputTokens - entry.CacheRead
	if regularInput < 0 {
		regularInput = 0
	}

	// Separate thinking tokens from regular output.
	regularOutput := entry.OutputTokens - entry.ThinkingTokens
	if regularOutput < 0 {
		regularOutput = 0
	}
	thinkingCost := float64(entry.ThinkingTokens) / 1_000_000 * pricing.EffectiveThinkingCost()

	cost = float64(regularInput)/1_000_000*pricing.InputCost +
		float64(regularOutput)/1_000_000*pricing.OutputCost +
		thinkingCost +
		float64(entry.CacheCreation)/1_000_000*pricing.InputCost*1.25 + // Cache creation costs 25% more
		float64(entry.CacheRead)/1_000_000*pricing.CacheCost

	// What it would have cost without caching.
	fullCost := float64(entry.InputTokens)/1_000_000*pricing.InputCost +
		float64(regularOutput)/1_000_000*pricing.OutputCost +
		thinkingCost
	saved = fullCost - cost
	if saved < 0 {
		saved = 0
	}
	return
}

// resolvePricing finds pricing for a model: PricingCache → KnownModels → Sonnet fallback.
func (be *BudgetEngine) resolvePricing(modelID string) *ModelPricing {
	// Try live pricing from OpenRouter.
	if be.pricingCache != nil {
		if p := be.pricingCache.FindModelPricing(modelID); p != nil {
			return p
		}
	}

	// Try KnownModels.
	if m := FindModel(modelID); m != nil {
		return &ModelPricing{
			InputCost:    m.InputCost,
			OutputCost:   m.OutputCost,
			CacheCost:    m.CacheCost,
			ThinkingCost: m.ThinkingCost,
		}
	}

	// Sonnet fallback.
	return &ModelPricing{
		InputCost:  3.0,
		OutputCost: 15.0,
		CacheCost:  0.3,
	}
}

func (be *BudgetEngine) checkLimits(ctx context.Context) error {
	be.mu.RLock()
	config := be.config
	be.mu.RUnlock()

	summary := be.getSummaryInternal(ctx)

	// Check daily limit.
	if config.DailyLimitUSD > 0 {
		pct := summary.TodayUSD / config.DailyLimitUSD * 100
		if pct >= 100 {
			be.fireAlert(ctx, "limit_reached", "daily", config.DailyLimitUSD, summary.TodayUSD, pct)
			if config.AlertAtPercent > 0 {
				// Hard stop check.
				var hardStop int
				be.db.QueryRow(`SELECT hard_stop FROM budgets WHERE id = 'default'`).Scan(&hardStop)
				if hardStop == 1 {
					return fmt.Errorf("daily budget exceeded: $%.2f / $%.2f", summary.TodayUSD, config.DailyLimitUSD)
				}
			}
		} else if pct >= config.AlertAtPercent {
			be.fireAlert(ctx, "warning", "daily", config.DailyLimitUSD, summary.TodayUSD, pct)
		}
	}

	// Check monthly limit.
	if config.MonthlyLimitUSD > 0 {
		pct := summary.MonthUSD / config.MonthlyLimitUSD * 100
		if pct >= 100 {
			be.fireAlert(ctx, "limit_reached", "monthly", config.MonthlyLimitUSD, summary.MonthUSD, pct)
			var hardStop int
			be.db.QueryRow(`SELECT hard_stop FROM budgets WHERE id = 'default'`).Scan(&hardStop)
			if hardStop == 1 {
				return fmt.Errorf("monthly budget exceeded: $%.2f / $%.2f", summary.MonthUSD, config.MonthlyLimitUSD)
			}
		} else if pct >= config.AlertAtPercent {
			be.fireAlert(ctx, "warning", "monthly", config.MonthlyLimitUSD, summary.MonthUSD, pct)
		}
	}

	return nil
}

func (be *BudgetEngine) fireAlert(ctx context.Context, alertType, period string, limit, spent, pct float64) {
	// Don't fire duplicate alerts within the same hour.
	var count int
	be.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM budget_alerts
		 WHERE alert_type = ? AND period = ? AND created_at > datetime('now', '-1 hour')`,
		alertType, period).Scan(&count)
	if count > 0 {
		return
	}

	be.db.ExecContext(ctx,
		`INSERT INTO budget_alerts (alert_type, period, limit_usd, spent_usd, percent) VALUES (?, ?, ?, ?, ?)`,
		alertType, period, limit, spent, pct)

	log.Printf("budget: %s alert — %s spending $%.2f / $%.2f (%.0f%%)", alertType, period, spent, limit, pct)
}

// GetSummary returns current spending summary.
func (be *BudgetEngine) GetSummary(ctx context.Context) SpendingSummary {
	be.mu.RLock()
	config := be.config
	be.mu.RUnlock()

	summary := be.getSummaryInternal(ctx)
	summary.DailyLimitUSD = config.DailyLimitUSD
	summary.MonthlyLimitUSD = config.MonthlyLimitUSD
	summary.AlertAtPercent = config.AlertAtPercent

	var hardStop int
	be.db.QueryRow(`SELECT hard_stop FROM budgets WHERE id = 'default'`).Scan(&hardStop)
	summary.HardStop = hardStop == 1

	if config.DailyLimitUSD > 0 {
		summary.DailyPercent = summary.TodayUSD / config.DailyLimitUSD * 100
		summary.DailyRemaining = config.DailyLimitUSD - summary.TodayUSD
		if summary.DailyRemaining < 0 {
			summary.DailyRemaining = 0
		}
	}
	if config.MonthlyLimitUSD > 0 {
		summary.MonthlyPercent = summary.MonthUSD / config.MonthlyLimitUSD * 100
		summary.MonthlyRemaining = config.MonthlyLimitUSD - summary.MonthUSD
		if summary.MonthlyRemaining < 0 {
			summary.MonthlyRemaining = 0
		}
	}

	return summary
}

func (be *BudgetEngine) getSummaryInternal(ctx context.Context) SpendingSummary {
	var s SpendingSummary
	// Use UTC to match SQLite's datetime('now') which stores in UTC.
	today := time.Now().UTC().Format("2006-01-02")
	monthStart := time.Now().UTC().Format("2006-01") + "-01"

	// Today's spending.
	be.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0), COUNT(*), COALESCE(SUM(saved_usd), 0)
		 FROM cost_log WHERE created_at >= ?`, today).
		Scan(&s.TodayUSD, &s.TodayRequests, &s.TodaySavedUSD)

	// This month's spending.
	be.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0), COUNT(*), COALESCE(SUM(saved_usd), 0)
		 FROM cost_log WHERE created_at >= ?`, monthStart).
		Scan(&s.MonthUSD, &s.MonthRequests, &s.MonthSavedUSD)

	return s
}

// GetDailyCosts returns cost data per day for the last N days.
func (be *BudgetEngine) GetDailyCosts(ctx context.Context, days int) []DailyCost {
	if days <= 0 {
		days = 30
	}

	rows, err := be.db.QueryContext(ctx,
		`SELECT date(created_at) as day, SUM(cost_usd), SUM(saved_usd), COUNT(*)
		 FROM cost_log
		 WHERE created_at >= date('now', ?)
		 GROUP BY day ORDER BY day`,
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var costs []DailyCost
	for rows.Next() {
		var dc DailyCost
		rows.Scan(&dc.Date, &dc.CostUSD, &dc.SavedUSD, &dc.Requests)
		costs = append(costs, dc)
	}
	return costs
}

// GetAlerts returns recent budget alerts.
func (be *BudgetEngine) GetAlerts(ctx context.Context, limit int) []BudgetAlert {
	if limit <= 0 {
		limit = 20
	}

	rows, err := be.db.QueryContext(ctx,
		`SELECT id, alert_type, period, limit_usd, spent_usd, percent, acknowledged, created_at
		 FROM budget_alerts ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var alerts []BudgetAlert
	for rows.Next() {
		var a BudgetAlert
		var ack int
		rows.Scan(&a.ID, &a.AlertType, &a.Period, &a.LimitUSD, &a.SpentUSD, &a.Percent, &ack, &a.CreatedAt)
		a.Acknowledged = ack == 1
		alerts = append(alerts, a)
	}
	return alerts
}

// GetUnacknowledgedAlerts returns alerts that haven't been dismissed.
func (be *BudgetEngine) GetUnacknowledgedAlerts(ctx context.Context) []BudgetAlert {
	rows, err := be.db.QueryContext(ctx,
		`SELECT id, alert_type, period, limit_usd, spent_usd, percent, acknowledged, created_at
		 FROM budget_alerts WHERE acknowledged = 0 ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var alerts []BudgetAlert
	for rows.Next() {
		var a BudgetAlert
		var ack int
		rows.Scan(&a.ID, &a.AlertType, &a.Period, &a.LimitUSD, &a.SpentUSD, &a.Percent, &ack, &a.CreatedAt)
		a.Acknowledged = ack == 1
		alerts = append(alerts, a)
	}
	return alerts
}

// AcknowledgeAlert marks an alert as acknowledged.
func (be *BudgetEngine) AcknowledgeAlert(ctx context.Context, id int) error {
	_, err := be.db.ExecContext(ctx, `UPDATE budget_alerts SET acknowledged = 1 WHERE id = ?`, id)
	return err
}

// UpdateConfig updates the budget configuration.
func (be *BudgetEngine) UpdateConfig(ctx context.Context, daily, monthly, alertPct float64, hardStop bool) error {
	hs := 0
	if hardStop {
		hs = 1
	}

	_, err := be.db.ExecContext(ctx,
		`UPDATE budgets SET daily_limit_usd = ?, monthly_limit_usd = ?, alert_at_percent = ?, hard_stop = ?, updated_at = datetime('now')
		 WHERE id = 'default'`,
		daily, monthly, alertPct, hs)
	if err != nil {
		return err
	}

	be.mu.Lock()
	be.config.DailyLimitUSD = daily
	be.config.MonthlyLimitUSD = monthly
	be.config.AlertAtPercent = alertPct
	be.mu.Unlock()

	return nil
}

// GetConfig returns the current budget config.
func (be *BudgetEngine) GetConfig() BudgetConfig {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return be.config
}

// GetTopModels returns the top N models by cost.
func (be *BudgetEngine) GetTopModels(ctx context.Context, limit int) []map[string]any {
	if limit <= 0 {
		limit = 5
	}

	rows, err := be.db.QueryContext(ctx,
		`SELECT model, provider, SUM(cost_usd) as total, COUNT(*) as requests,
			SUM(input_tokens) as input, SUM(output_tokens) as output
		 FROM cost_log
		 WHERE created_at >= date('now', 'start of month')
		 GROUP BY model, provider ORDER BY total DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var models []map[string]any
	for rows.Next() {
		var model, prov string
		var total float64
		var requests, input, output int
		rows.Scan(&model, &prov, &total, &requests, &input, &output)
		models = append(models, map[string]any{
			"model": model, "provider": prov, "cost_usd": total,
			"requests": requests, "input_tokens": input, "output_tokens": output,
		})
	}
	return models
}
