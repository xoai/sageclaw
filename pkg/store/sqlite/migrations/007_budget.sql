-- Cost log: per-request cost tracking.
CREATE TABLE IF NOT EXISTS cost_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation INTEGER NOT NULL DEFAULT 0,
    cache_read INTEGER NOT NULL DEFAULT 0,
    cost_usd REAL NOT NULL DEFAULT 0.0,
    saved_usd REAL NOT NULL DEFAULT 0.0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_cost_log_date ON cost_log(created_at);
CREATE INDEX IF NOT EXISTS idx_cost_log_agent ON cost_log(agent_id);

-- Budget configuration.
CREATE TABLE IF NOT EXISTS budgets (
    id TEXT PRIMARY KEY DEFAULT 'default',
    daily_limit_usd REAL NOT NULL DEFAULT 0.0,
    monthly_limit_usd REAL NOT NULL DEFAULT 0.0,
    alert_at_percent REAL NOT NULL DEFAULT 80.0,
    hard_stop INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Budget alerts history.
CREATE TABLE IF NOT EXISTS budget_alerts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_type TEXT NOT NULL,
    period TEXT NOT NULL,
    limit_usd REAL NOT NULL,
    spent_usd REAL NOT NULL,
    percent REAL NOT NULL,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Insert default budget (no limits).
INSERT OR IGNORE INTO budgets (id, daily_limit_usd, monthly_limit_usd, alert_at_percent, hard_stop)
VALUES ('default', 0.0, 0.0, 80.0, 0);
