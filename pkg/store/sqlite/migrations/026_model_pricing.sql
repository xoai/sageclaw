-- Add pricing columns to discovered_models.
ALTER TABLE discovered_models ADD COLUMN input_cost REAL DEFAULT 0;
ALTER TABLE discovered_models ADD COLUMN output_cost REAL DEFAULT 0;
ALTER TABLE discovered_models ADD COLUMN cache_cost REAL DEFAULT 0;
ALTER TABLE discovered_models ADD COLUMN thinking_cost REAL DEFAULT 0;
ALTER TABLE discovered_models ADD COLUMN cache_creation_cost REAL DEFAULT 0;
ALTER TABLE discovered_models ADD COLUMN pricing_source TEXT DEFAULT '';

-- User pricing overrides: highest-precedence pricing, survives discovery refresh.
CREATE TABLE IF NOT EXISTS model_pricing_overrides (
    model_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    input_cost REAL NOT NULL DEFAULT 0,
    output_cost REAL NOT NULL DEFAULT 0,
    cache_cost REAL DEFAULT 0,
    thinking_cost REAL DEFAULT 0,
    cache_creation_cost REAL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
