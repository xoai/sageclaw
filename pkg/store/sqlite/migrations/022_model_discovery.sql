-- Model discovery cache: stores models fetched from provider APIs.
CREATE TABLE IF NOT EXISTS discovered_models (
    id TEXT PRIMARY KEY,                -- "anthropic/claude-sonnet-4-20250514"
    provider TEXT NOT NULL,             -- "anthropic", "openai", "gemini", "ollama"
    model_id TEXT NOT NULL,             -- Raw API ID: "claude-sonnet-4-20250514"
    display_name TEXT NOT NULL,         -- "Claude Sonnet 4"
    context_window INTEGER DEFAULT 0,   -- Max input tokens
    max_output_tokens INTEGER DEFAULT 0,-- Max output tokens
    capabilities TEXT DEFAULT '{}',     -- JSON: {"vision":true,"thinking":true,...}
    discovered_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_discovered_models_provider ON discovered_models(provider);

-- Remove hardcoded preset combos. Tiers now resolve dynamically.
DELETE FROM combos WHERE is_preset = 1;
