-- Migration 016: Add confidence scores to memories.
--
-- Confidence levels:
--   0.9 — User correction (self-learning, high trust)
--   0.8 — Explicit user statement (default)
--   0.7 — General fact from conversation
--   0.5 — Inferred preference
--
-- Search queries weight by confidence * recency for ranking.
ALTER TABLE memories ADD COLUMN confidence REAL NOT NULL DEFAULT 0.8;
