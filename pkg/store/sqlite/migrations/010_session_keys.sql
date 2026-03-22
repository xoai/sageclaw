-- Session architecture redesign: composite keys + metadata

-- Add new columns to sessions table.
ALTER TABLE sessions ADD COLUMN key TEXT;
ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'direct';
ALTER TABLE sessions ADD COLUMN label TEXT;
ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE sessions ADD COLUMN model TEXT;
ALTER TABLE sessions ADD COLUMN provider TEXT;
ALTER TABLE sessions ADD COLUMN spawned_by TEXT;
ALTER TABLE sessions ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN compaction_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN message_count INTEGER NOT NULL DEFAULT 0;

-- Backfill composite keys from existing data.
UPDATE sessions SET key = agent_id || ':' || channel || ':direct:' || chat_id
WHERE key IS NULL;

-- Backfill message counts.
UPDATE sessions SET message_count = (
    SELECT COUNT(*) FROM messages WHERE messages.session_id = sessions.id
);

-- Create unique index on key.
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_key ON sessions(key);

-- Index for listing by agent, channel, status.
CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_sessions_channel ON sessions(channel, status);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status, updated_at);
