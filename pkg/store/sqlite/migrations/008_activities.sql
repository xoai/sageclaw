-- Activities: user-facing unit of work with lifecycle.
CREATE TABLE IF NOT EXISTS activities (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    summary         TEXT,
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    cache_creation  INTEGER NOT NULL DEFAULT 0,
    cache_read      INTEGER NOT NULL DEFAULT 0,
    cost_usd        REAL NOT NULL DEFAULT 0.0,
    iterations      INTEGER NOT NULL DEFAULT 0,
    tool_calls      INTEGER NOT NULL DEFAULT 0,
    parent_id       TEXT REFERENCES activities(id),
    error_message   TEXT,
    started_at      TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at    TEXT,
    timeout_seconds INTEGER NOT NULL DEFAULT 300
);
CREATE INDEX IF NOT EXISTS idx_activities_session ON activities(session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_activities_status ON activities(status);
CREATE INDEX IF NOT EXISTS idx_activities_parent ON activities(parent_id);
