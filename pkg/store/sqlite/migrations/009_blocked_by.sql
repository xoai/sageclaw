-- Add blocked_by column to team_tasks (was in Go struct but missing from schema).
ALTER TABLE team_tasks ADD COLUMN blocked_by TEXT NOT NULL DEFAULT '';
