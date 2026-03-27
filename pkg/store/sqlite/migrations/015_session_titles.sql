-- Migration 015: Add title column to sessions.
-- Auto-generated from first user message (first 80 chars).
ALTER TABLE sessions ADD COLUMN title TEXT DEFAULT '';
