-- Migration 021: Extend team tables for multi-agent team orchestration.
-- Non-destructive: adds columns to existing tables, creates new table.
-- Note: blocked_by column already exists from migration 009.

-- Step 1: Extend teams table (one ALTER per column, SQLite requirement).
ALTER TABLE teams ADD COLUMN description TEXT DEFAULT '';
ALTER TABLE teams ADD COLUMN status TEXT DEFAULT 'active';
ALTER TABLE teams ADD COLUMN settings TEXT DEFAULT '{}';
ALTER TABLE teams ADD COLUMN created_at TEXT;
ALTER TABLE teams ADD COLUMN updated_at TEXT;

-- Step 2: Extend team_tasks with new columns.
ALTER TABLE team_tasks ADD COLUMN parent_id TEXT REFERENCES team_tasks(id);
ALTER TABLE team_tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN owner_agent_id TEXT;
ALTER TABLE team_tasks ADD COLUMN batch_id TEXT;
ALTER TABLE team_tasks ADD COLUMN task_number INTEGER DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN identifier TEXT;
ALTER TABLE team_tasks ADD COLUMN progress_percent INTEGER DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN require_approval INTEGER DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN session_id TEXT;
ALTER TABLE team_tasks ADD COLUMN retry_count INTEGER DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN max_retries INTEGER DEFAULT 1;
ALTER TABLE team_tasks ADD COLUMN error_message TEXT;
ALTER TABLE team_tasks ADD COLUMN completed_at TEXT;

-- Step 3: Migrate status values from v1 to v2 naming.
UPDATE team_tasks SET status = 'pending' WHERE status = 'open';
UPDATE team_tasks SET status = 'in_progress' WHERE status = 'claimed';

-- Step 4: Add indexes for new query patterns.
CREATE INDEX IF NOT EXISTS idx_team_tasks_parent ON team_tasks(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_team_tasks_batch ON team_tasks(batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_team_tasks_blocked ON team_tasks(team_id, status) WHERE status = 'blocked';
CREATE UNIQUE INDEX IF NOT EXISTS idx_team_tasks_identifier ON team_tasks(team_id, identifier) WHERE identifier IS NOT NULL;

-- Step 5: New table for task comments (audit trail).
CREATE TABLE IF NOT EXISTS team_task_comments (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES team_tasks(id),
    agent_id TEXT,
    user_id TEXT,
    content TEXT NOT NULL,
    comment_type TEXT DEFAULT 'note',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_task_comments_task ON team_task_comments(task_id);
