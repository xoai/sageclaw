package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// UpsertDiscoveredModels inserts or replaces discovered models in the cache.
// Pricing columns are only written when PricingSource is non-empty, preserving
// existing pricing from other sources (OpenRouter, user overrides).
func (s *Store) UpsertDiscoveredModels(ctx context.Context, models []store.DiscoveredModel) error {
	for _, m := range models {
		capsJSON, _ := json.Marshal(m.Capabilities)
		if m.PricingSource != "" {
			// Caller provides pricing — write all columns.
			_, err := s.db.ExecContext(ctx,
				`INSERT INTO discovered_models (id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, input_cost, output_cost, cache_cost, thinking_cost, cache_creation_cost, pricing_source, discovered_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
				 ON CONFLICT(id) DO UPDATE SET
				   display_name=excluded.display_name,
				   context_window=excluded.context_window,
				   max_output_tokens=excluded.max_output_tokens,
				   capabilities=excluded.capabilities,
				   input_cost=excluded.input_cost,
				   output_cost=excluded.output_cost,
				   cache_cost=excluded.cache_cost,
				   thinking_cost=excluded.thinking_cost,
				   cache_creation_cost=excluded.cache_creation_cost,
				   pricing_source=excluded.pricing_source,
				   updated_at=datetime('now')`,
				m.ID, m.Provider, m.ModelID, m.DisplayName, m.ContextWindow, m.MaxOutputTokens, string(capsJSON),
				m.InputCost, m.OutputCost, m.CacheCost, m.ThinkingCost, m.CacheCreationCost, m.PricingSource)
			if err != nil {
				return err
			}
		} else {
			// Discovery-only upsert — preserve existing pricing columns.
			_, err := s.db.ExecContext(ctx,
				`INSERT INTO discovered_models (id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, discovered_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
				 ON CONFLICT(id) DO UPDATE SET
				   display_name=excluded.display_name,
				   context_window=excluded.context_window,
				   max_output_tokens=excluded.max_output_tokens,
				   capabilities=excluded.capabilities,
				   updated_at=datetime('now')`,
				m.ID, m.Provider, m.ModelID, m.DisplayName, m.ContextWindow, m.MaxOutputTokens, string(capsJSON))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

const discoveredModelsCols = `id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, discovered_at, updated_at, input_cost, output_cost, cache_cost, thinking_cost, cache_creation_cost, pricing_source`

// ListDiscoveredModels returns cached models for a specific provider.
func (s *Store) ListDiscoveredModels(ctx context.Context, provider string) ([]store.DiscoveredModel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+discoveredModelsCols+` FROM discovered_models WHERE provider = ? ORDER BY display_name`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiscoveredModels(rows)
}

// ListAllDiscoveredModels returns all cached models across all providers.
func (s *Store) ListAllDiscoveredModels(ctx context.Context) ([]store.DiscoveredModel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+discoveredModelsCols+` FROM discovered_models ORDER BY provider, display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiscoveredModels(rows)
}

// DeleteDiscoveredModelsByProvider removes all cached models for a provider.
func (s *Store) DeleteDiscoveredModelsByProvider(ctx context.Context, provider string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM discovered_models WHERE provider = ?`, provider)
	return err
}

// RefreshDiscoveredModels atomically refreshes models for a provider.
// Uses upsert (preserving pricing columns) + selective DELETE for removed models.
// Only updates capability columns (display_name, context_window, etc.) — pricing
// columns are intentionally preserved. To update pricing, use UpsertDiscoveredModels
// with PricingSource set.
func (s *Store) RefreshDiscoveredModels(ctx context.Context, provider string, models []store.DiscoveredModel) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Upsert each model — only update capability columns, preserve pricing.
	ids := make([]any, 0, len(models))
	for _, m := range models {
		capsJSON, _ := json.Marshal(m.Capabilities)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO discovered_models (id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, discovered_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
			 ON CONFLICT(id) DO UPDATE SET
			   display_name=excluded.display_name,
			   context_window=excluded.context_window,
			   max_output_tokens=excluded.max_output_tokens,
			   capabilities=excluded.capabilities,
			   updated_at=datetime('now')`,
			m.ID, m.Provider, m.ModelID, m.DisplayName, m.ContextWindow, m.MaxOutputTokens, string(capsJSON)); err != nil {
			return err
		}
		ids = append(ids, m.ID)
	}

	// Delete models no longer reported by the provider.
	if len(ids) > 0 {
		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
		args := append([]any{provider}, ids...)
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM discovered_models WHERE provider = ? AND id NOT IN (`+placeholders+`)`,
			args...); err != nil {
			return err
		}
	} else {
		// Provider reported zero models — remove all.
		if _, err := tx.ExecContext(ctx, `DELETE FROM discovered_models WHERE provider = ?`, provider); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetDiscoveredModelAge returns how old the cached models are for a provider.
// If no models are cached, returns a large duration to force refresh.
func (s *Store) GetDiscoveredModelAge(ctx context.Context, provider string) (time.Duration, error) {
	var updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(updated_at) FROM discovered_models WHERE provider = ?`, provider).Scan(&updatedAt)
	if err != nil || updatedAt == "" {
		return 365 * 24 * time.Hour, nil // Force refresh.
	}
	t, err := time.Parse("2006-01-02 15:04:05", updatedAt)
	if err != nil {
		return 365 * 24 * time.Hour, nil
	}
	return time.Since(t), nil
}

func scanDiscoveredModels(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]store.DiscoveredModel, error) {
	var models []store.DiscoveredModel
	for rows.Next() {
		var m store.DiscoveredModel
		var capsJSON, discoveredAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName,
			&m.ContextWindow, &m.MaxOutputTokens, &capsJSON, &discoveredAt, &updatedAt,
			&m.InputCost, &m.OutputCost, &m.CacheCost, &m.ThinkingCost,
			&m.CacheCreationCost, &m.PricingSource); err != nil {
			return nil, err
		}
		m.Capabilities = make(map[string]bool)
		json.Unmarshal([]byte(capsJSON), &m.Capabilities)
		m.DiscoveredAt, _ = time.Parse("2006-01-02 15:04:05", discoveredAt)
		m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return models, nil
}
