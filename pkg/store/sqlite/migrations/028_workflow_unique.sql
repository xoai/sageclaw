-- Prevent duplicate active workflows for the same team+session (TOCTOU guard).
-- Clean up any existing duplicates first (keep the newest).
DELETE FROM team_workflows
WHERE id NOT IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (PARTITION BY team_id, session_id ORDER BY created_at DESC) AS rn
        FROM team_workflows
        WHERE state NOT IN ('complete', 'cancelled', 'failed')
    ) WHERE rn = 1
) AND state NOT IN ('complete', 'cancelled', 'failed');

-- Partial unique index: only one active workflow per team+session.
CREATE UNIQUE INDEX IF NOT EXISTS idx_team_workflows_active
ON team_workflows(team_id, session_id)
WHERE state NOT IN ('complete', 'cancelled', 'failed');
