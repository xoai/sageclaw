-- Migration 012: Channel Architecture v2
-- DM/group distinction, multi-field credentials, thread support.

-- Session kind rename: "direct" → "dm".
UPDATE sessions SET kind = 'dm' WHERE kind = 'direct';

-- Thread support on sessions.
ALTER TABLE sessions ADD COLUMN thread_id TEXT DEFAULT '';

-- Inline encrypted credentials on connections (multi-field support).
ALTER TABLE connections ADD COLUMN credentials BLOB;

-- DM/Group policy toggles per connection.
ALTER TABLE connections ADD COLUMN dm_enabled INTEGER DEFAULT 1;
ALTER TABLE connections ADD COLUMN group_enabled INTEGER DEFAULT 1;

-- Index for kind-aware session lookup.
CREATE INDEX IF NOT EXISTS idx_sessions_channel_kind ON sessions(channel, kind, chat_id);
