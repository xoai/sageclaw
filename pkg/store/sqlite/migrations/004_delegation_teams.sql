-- Delegation links (cached from YAML).
CREATE TABLE IF NOT EXISTS delegation_links (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    direction   TEXT NOT NULL DEFAULT 'sync',
    max_concurrent INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Delegation runtime state.
CREATE TABLE IF NOT EXISTS delegation_state (
    link_id     TEXT PRIMARY KEY REFERENCES delegation_links(id),
    active_count INTEGER NOT NULL DEFAULT 0
);

-- Delegation history.
CREATE TABLE IF NOT EXISTS delegation_history (
    id          TEXT PRIMARY KEY,
    link_id     TEXT NOT NULL,
    source_id   TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    result      TEXT,
    status      TEXT NOT NULL DEFAULT 'pending',
    started_at  TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_delegation_history_agent ON delegation_history(source_id, started_at);

-- Team definitions.
CREATE TABLE IF NOT EXISTS teams (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    lead_id     TEXT NOT NULL,
    config      TEXT NOT NULL DEFAULT '{}'
);

-- Team task board.
CREATE TABLE IF NOT EXISTS team_tasks (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id),
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open',
    assigned_to TEXT,
    created_by  TEXT NOT NULL,
    result      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_team_tasks_team ON team_tasks(team_id, status);

-- Team mailbox.
CREATE TABLE IF NOT EXISTS team_messages (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id),
    from_agent  TEXT NOT NULL,
    to_agent    TEXT,
    content     TEXT NOT NULL,
    read        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_team_messages_agent ON team_messages(to_agent, read);
