-- Voice messaging: audio duration tracking on activities.
ALTER TABLE activities ADD COLUMN audio_input_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE activities ADD COLUMN audio_output_ms INTEGER NOT NULL DEFAULT 0;
