package sqlite

import (
	"context"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// UpsertModelPricingOverride inserts or updates a user pricing override.
func (s *Store) UpsertModelPricingOverride(ctx context.Context, o store.ModelPricingOverride) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO model_pricing_overrides (model_id, provider, input_cost, output_cost, cache_cost, thinking_cost, cache_creation_cost, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(model_id) DO UPDATE SET
		   provider=excluded.provider,
		   input_cost=excluded.input_cost,
		   output_cost=excluded.output_cost,
		   cache_cost=excluded.cache_cost,
		   thinking_cost=excluded.thinking_cost,
		   cache_creation_cost=excluded.cache_creation_cost,
		   updated_at=datetime('now')`,
		o.ModelID, o.Provider, o.InputCost, o.OutputCost, o.CacheCost, o.ThinkingCost, o.CacheCreationCost)
	return err
}

// DeleteModelPricingOverride removes a user pricing override.
func (s *Store) DeleteModelPricingOverride(ctx context.Context, modelID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_pricing_overrides WHERE model_id = ?`, modelID)
	return err
}

// ListModelPricingOverrides returns all user pricing overrides.
func (s *Store) ListModelPricingOverrides(ctx context.Context) ([]store.ModelPricingOverride, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT model_id, provider, input_cost, output_cost, cache_cost, thinking_cost, cache_creation_cost, updated_at
		 FROM model_pricing_overrides ORDER BY model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overrides []store.ModelPricingOverride
	for rows.Next() {
		var o store.ModelPricingOverride
		var updatedAt string
		if err := rows.Scan(&o.ModelID, &o.Provider, &o.InputCost, &o.OutputCost,
			&o.CacheCost, &o.ThinkingCost, &o.CacheCreationCost, &updatedAt); err != nil {
			return nil, err
		}
		o.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		overrides = append(overrides, o)
	}
	return overrides, nil
}

// BulkUpdateModelPricing updates pricing columns on discovered_models.
// Only updates rows where pricing_source is not "user".
// For models not yet in discovered_models, creates minimal rows with pricing.
func (s *Store) BulkUpdateModelPricing(ctx context.Context, updates []store.ModelPricingBulk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO discovered_models (id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, input_cost, output_cost, cache_cost, thinking_cost, cache_creation_cost, pricing_source, discovered_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, '{}', ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
		   input_cost=excluded.input_cost,
		   output_cost=excluded.output_cost,
		   cache_cost=excluded.cache_cost,
		   thinking_cost=excluded.thinking_cost,
		   cache_creation_cost=excluded.cache_creation_cost,
		   context_window=CASE WHEN excluded.context_window > 0 THEN excluded.context_window ELSE discovered_models.context_window END,
		   max_output_tokens=CASE WHEN excluded.max_output_tokens > 0 THEN excluded.max_output_tokens ELSE discovered_models.max_output_tokens END,
		   pricing_source=excluded.pricing_source,
		   updated_at=datetime('now')
		 WHERE discovered_models.pricing_source IS NULL
		    OR discovered_models.pricing_source = ''
		    OR discovered_models.pricing_source = 'known'
		    OR discovered_models.pricing_source = 'openrouter'`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, u := range updates {
		id := u.ModelID
		if u.Provider != "" {
			id = u.Provider + "/" + u.ModelID
		}
		_, err := stmt.ExecContext(ctx, id, u.Provider, u.ModelID, u.ModelID,
			u.ContextWindow, u.MaxOutputTokens,
			u.InputCost, u.OutputCost, u.CacheCost, u.ThinkingCost, u.CacheCreationCost, u.Source)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetModelPricing resolves pricing for a model. Precedence: user override > discovered > nil.
func (s *Store) GetModelPricing(ctx context.Context, modelID string) (*store.DiscoveredModel, error) {
	// Check user override first.
	var o store.ModelPricingOverride
	var updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT model_id, provider, input_cost, output_cost, cache_cost, thinking_cost, cache_creation_cost, updated_at
		 FROM model_pricing_overrides WHERE model_id = ?`, modelID).
		Scan(&o.ModelID, &o.Provider, &o.InputCost, &o.OutputCost,
			&o.CacheCost, &o.ThinkingCost, &o.CacheCreationCost, &updatedAt)
	if err == nil {
		// Return override as a DiscoveredModel-shaped result.
		return &store.DiscoveredModel{
			ID:                modelID,
			Provider:          o.Provider,
			ModelID:           modelID,
			InputCost:         o.InputCost,
			OutputCost:        o.OutputCost,
			CacheCost:         o.CacheCost,
			ThinkingCost:      o.ThinkingCost,
			CacheCreationCost: o.CacheCreationCost,
			PricingSource:     "user",
		}, nil
	}

	// Fall back to discovered model pricing.
	var m store.DiscoveredModel
	var capsJSON, discoveredAt, updatedAt2 string
	err = s.db.QueryRowContext(ctx,
		`SELECT `+discoveredModelsCols+` FROM discovered_models WHERE id = ? AND pricing_source != ''`,
		modelID).
		Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName,
			&m.ContextWindow, &m.MaxOutputTokens, &capsJSON, &discoveredAt, &updatedAt2,
			&m.InputCost, &m.OutputCost, &m.CacheCost, &m.ThinkingCost,
			&m.CacheCreationCost, &m.PricingSource)
	if err == nil {
		return &m, nil
	}

	// Also try matching by model_id (raw API ID) for cases where
	// callers use "claude-sonnet-4-20250514" instead of "anthropic/claude-sonnet-4-20250514".
	// LIMIT 1 + ORDER BY updated_at DESC ensures deterministic result if multiple providers
	// have models with the same raw ID.
	err = s.db.QueryRowContext(ctx,
		`SELECT `+discoveredModelsCols+` FROM discovered_models WHERE model_id = ? AND pricing_source != '' ORDER BY updated_at DESC LIMIT 1`,
		modelID).
		Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName,
			&m.ContextWindow, &m.MaxOutputTokens, &capsJSON, &discoveredAt, &updatedAt2,
			&m.InputCost, &m.OutputCost, &m.CacheCost, &m.ThinkingCost,
			&m.CacheCreationCost, &m.PricingSource)
	if err == nil {
		return &m, nil
	}

	return nil, nil // Unknown model — cost will be $0.
}
