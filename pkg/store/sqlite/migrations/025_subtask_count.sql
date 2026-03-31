-- +migrate Up
ALTER TABLE team_tasks ADD COLUMN subtask_count INTEGER DEFAULT 0;

-- +migrate Down
-- SQLite does not support DROP COLUMN in older versions.
-- This column is safe to leave in place (additive-only migration).
