-- +migrate Up
ALTER TABLE team_tasks ADD COLUMN claimed_at TEXT;
ALTER TABLE team_tasks ADD COLUMN dispatch_attempts INTEGER NOT NULL DEFAULT 0;

-- +migrate Down
-- SQLite does not support DROP COLUMN in older versions.
-- These columns are safe to leave in place (additive-only migration).
