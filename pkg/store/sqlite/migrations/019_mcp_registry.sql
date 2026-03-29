-- Migration 019: MCP marketplace registry.
-- Stores curated and user-installed MCP servers with metadata,
-- install state, and agent assignment.

CREATE TABLE IF NOT EXISTS mcp_registry (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    description   TEXT,
    category      TEXT,
    connection    TEXT NOT NULL,       -- JSON: ConnectionConfig
    fallback_conn TEXT,                -- JSON: ConnectionConfig (nullable)
    config_schema TEXT DEFAULT '{}',   -- JSON: map of ConfigField
    github_url    TEXT,
    stars         INTEGER DEFAULT 0,
    tags          TEXT DEFAULT '[]',   -- JSON string array
    source        TEXT NOT NULL DEFAULT 'curated',  -- 'curated' | 'custom'
    installed     INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 0,
    agent_ids     TEXT DEFAULT '[]',   -- JSON string array
    installed_at  TEXT,
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_mcp_reg_category ON mcp_registry(category);
CREATE INDEX IF NOT EXISTS idx_mcp_reg_installed ON mcp_registry(installed)
    WHERE installed = 1;

-- Track curated index seed version for incremental updates.
CREATE TABLE IF NOT EXISTS mcp_seed_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT OR IGNORE INTO mcp_seed_meta (key, value) VALUES ('version', '0');
