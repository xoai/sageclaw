-- Multi-channel connections: connection-ID-keyed channel management.

CREATE TABLE connections (
    id             TEXT PRIMARY KEY,
    platform       TEXT NOT NULL,
    agent_id       TEXT,
    label          TEXT NOT NULL DEFAULT '',
    metadata       TEXT NOT NULL DEFAULT '{}',
    credential_key TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_connections_platform ON connections(platform);
CREATE INDEX idx_connections_agent ON connections(agent_id);

-- Track which connection a session belongs to.
ALTER TABLE sessions ADD COLUMN connection_id TEXT DEFAULT '';
