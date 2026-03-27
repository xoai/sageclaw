-- Migration 014: Replace FTS5 Porter stemmer with plain unicode61 tokenizer.
--
-- Porter stemmer is English-only and corrupts Vietnamese text:
-- - "nhà hàng" (restaurant) stems to nonsense
-- - Diacritics conflated: "ma" (ghost) vs "mà" (but) vs "má" (mother)
--
-- Trade-off: English search loses stemming ("running" won't match "run")
-- but Vietnamese search quality improves dramatically.
--
-- This requires dropping and recreating the FTS5 table + triggers,
-- then re-indexing all existing memories.

-- Drop existing triggers first.
DROP TRIGGER IF EXISTS memories_ai;
DROP TRIGGER IF EXISTS memories_ad;
DROP TRIGGER IF EXISTS memories_au;

-- Drop and recreate FTS5 table with unicode61 (no porter).
DROP TABLE IF EXISTS memories_fts;

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    title,
    content,
    tags,
    content='memories',
    content_rowid='rowid',
    tokenize='unicode61'
);

-- Recreate sync triggers.
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, title, content, tags)
    VALUES (new.rowid, new.title, new.content, new.tags);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, title, content, tags)
    VALUES ('delete', old.rowid, old.title, old.content, old.tags);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, title, content, tags)
    VALUES ('delete', old.rowid, old.title, old.content, old.tags);
    INSERT INTO memories_fts(rowid, title, content, tags)
    VALUES (new.rowid, new.title, new.content, new.tags);
END;

-- Re-index all existing memories into the new FTS5 table.
INSERT INTO memories_fts(rowid, title, content, tags)
    SELECT rowid, title, content, tags FROM memories;
