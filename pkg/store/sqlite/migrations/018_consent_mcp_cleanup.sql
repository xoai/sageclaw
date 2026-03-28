-- Migration 018: Clean up blanket MCP consent grants.
-- The new tool access model uses per-server consent keys (e.g. "mcp:weather")
-- instead of the blanket "mcp" group. Delete old blanket grants so users
-- re-consent per server.

DELETE FROM consent_grants WHERE tool_group = 'mcp';

-- Add UNIQUE constraint for upsert support (if not already present).
-- SQLite does not support ADD CONSTRAINT, so we check via index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_consent_grants_upsert
    ON consent_grants(owner_id, platform, tool_group);
