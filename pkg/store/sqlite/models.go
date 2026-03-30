package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// UpsertDiscoveredModels inserts or replaces discovered models in the cache.
func (s *Store) UpsertDiscoveredModels(ctx context.Context, models []store.DiscoveredModel) error {
	for _, m := range models {
		capsJSON, _ := json.Marshal(m.Capabilities)
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
	return nil
}

// ListDiscoveredModels returns cached models for a specific provider.
func (s *Store) ListDiscoveredModels(ctx context.Context, provider string) ([]store.DiscoveredModel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, discovered_at, updated_at
		 FROM discovered_models WHERE provider = ? ORDER BY display_name`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiscoveredModels(rows)
}

// ListAllDiscoveredModels returns all cached models across all providers.
func (s *Store) ListAllDiscoveredModels(ctx context.Context) ([]store.DiscoveredModel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, discovered_at, updated_at
		 FROM discovered_models ORDER BY provider, display_name`)
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

// RefreshDiscoveredModels atomically replaces all models for a provider.
// Wraps delete+insert in a transaction so concurrent readers never see empty state.
func (s *Store) RefreshDiscoveredModels(ctx context.Context, provider string, models []store.DiscoveredModel) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM discovered_models WHERE provider = ?`, provider); err != nil {
		return err
	}

	for _, m := range models {
		capsJSON, _ := json.Marshal(m.Capabilities)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO discovered_models (id, provider, model_id, display_name, context_window, max_output_tokens, capabilities, discovered_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			m.ID, m.Provider, m.ModelID, m.DisplayName, m.ContextWindow, m.MaxOutputTokens, string(capsJSON)); err != nil {
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
}) ([]store.DiscoveredModel, error) {
	var models []store.DiscoveredModel
	for rows.Next() {
		var m store.DiscoveredModel
		var capsJSON, discoveredAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName,
			&m.ContextWindow, &m.MaxOutputTokens, &capsJSON, &discoveredAt, &updatedAt); err != nil {
			return nil, err
		}
		m.Capabilities = make(map[string]bool)
		json.Unmarshal([]byte(capsJSON), &m.Capabilities)
		m.DiscoveredAt, _ = time.Parse("2006-01-02 15:04:05", discoveredAt)
		m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		models = append(models, m)
	}
	return models, nil
}
