-- Add thinking_tokens column to cost_log for tracking reasoning/thinking token costs.
ALTER TABLE cost_log ADD COLUMN thinking_tokens INTEGER DEFAULT 0;
