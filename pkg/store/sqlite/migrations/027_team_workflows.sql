-- Team workflow engine: deterministic state machine for team delegation.
CREATE TABLE IF NOT EXISTS team_workflows (
    id TEXT PRIMARY KEY,
    team_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'analyze',
    version INTEGER NOT NULL DEFAULT 0,
    plan_json TEXT,
    task_ids TEXT,
    user_message TEXT,
    announcement TEXT,
    results_json TEXT,
    error TEXT,
    state_entered_at TEXT NOT NULL DEFAULT (datetime('now')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_team_workflows_team_session ON team_workflows(team_id, session_id);
CREATE INDEX IF NOT EXISTS idx_team_workflows_state ON team_workflows(state);
