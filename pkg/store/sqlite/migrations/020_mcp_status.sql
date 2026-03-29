-- Migration 020: Add status lifecycle to MCP registry.
-- Replaces installed/enabled booleans as the primary state indicator.
-- Booleans kept for backward compat, derived from status.

ALTER TABLE mcp_registry ADD COLUMN status TEXT NOT NULL DEFAULT 'available';
ALTER TABLE mcp_registry ADD COLUMN status_error TEXT DEFAULT '';

-- Backfill from existing booleans.
UPDATE mcp_registry SET status = 'connected' WHERE installed = 1 AND enabled = 1;
UPDATE mcp_registry SET status = 'disabled' WHERE installed = 1 AND enabled = 0;
