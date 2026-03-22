-- Providers: configured LLM providers with encrypted API keys.
CREATE TABLE IF NOT EXISTS providers (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,  -- anthropic, openai, ollama
    name TEXT NOT NULL,
    base_url TEXT NOT NULL DEFAULT '',
    api_key_enc BLOB,  -- AES-256-GCM encrypted
    models TEXT NOT NULL DEFAULT '[]',  -- JSON array of available model IDs
    status TEXT NOT NULL DEFAULT 'active',
    config TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Combos: model routing presets (like sage-router strategies).
CREATE TABLE IF NOT EXISTS combos (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    strategy TEXT NOT NULL DEFAULT 'priority',  -- priority, round-robin, cost
    models TEXT NOT NULL DEFAULT '[]',  -- JSON array of {provider_id, model_id, priority, weight}
    is_preset INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Seed preset combos.
INSERT OR IGNORE INTO combos (id, name, description, strategy, models, is_preset) VALUES
    ('strong', 'Strong', 'Best quality — Claude Sonnet / GPT-4o', 'priority',
     '[{"provider_type":"anthropic","model":"claude-sonnet-4-20250514","priority":1},{"provider_type":"openai","model":"gpt-4o","priority":2}]', 1),
    ('fast', 'Fast', 'Low latency — Claude Haiku / GPT-4o-mini', 'priority',
     '[{"provider_type":"anthropic","model":"claude-haiku-4-5-20251001","priority":1},{"provider_type":"openai","model":"gpt-4o-mini","priority":2}]', 1),
    ('local', 'Local', 'Privacy-first — Ollama models only', 'priority',
     '[{"provider_type":"ollama","model":"llama3.2","priority":1}]', 1),
    ('balanced', 'Balanced', 'Cost/quality tradeoff — tries fast first, falls back to strong', 'priority',
     '[{"provider_type":"anthropic","model":"claude-haiku-4-5-20251001","priority":1},{"provider_type":"anthropic","model":"claude-sonnet-4-20250514","priority":2},{"provider_type":"openai","model":"gpt-4o","priority":3}]', 1);
