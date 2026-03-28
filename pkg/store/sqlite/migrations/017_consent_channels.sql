-- Migration 017: Cross-channel consent delivery.
--
-- Persistent consent grants (always-allow tier) survive restarts.
-- Owner identity on connections enables owner-only consent authorization.

-- Persistent consent grants.
CREATE TABLE IF NOT EXISTS consent_grants (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL,
    platform    TEXT NOT NULL,
    tool_group  TEXT NOT NULL,
    granted_at  TEXT NOT NULL DEFAULT (datetime('now')),
    revoked_at  TEXT,
    UNIQUE(owner_id, platform, tool_group)
);

CREATE INDEX idx_consent_grants_owner ON consent_grants(owner_id, platform);

-- Owner identity on connections.
ALTER TABLE connections ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT '';
