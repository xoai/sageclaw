-- Channel pairing: verified devices per channel.
CREATE TABLE IF NOT EXISTS paired_channels (
    channel     TEXT NOT NULL,
    chat_id     TEXT NOT NULL,
    paired_at   TEXT NOT NULL DEFAULT (datetime('now')),
    label       TEXT DEFAULT '',
    PRIMARY KEY (channel, chat_id)
);

-- Temporary pairing codes (expire after 10 minutes).
CREATE TABLE IF NOT EXISTS pairing_codes (
    code        TEXT PRIMARY KEY,
    channel     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT NOT NULL
);
