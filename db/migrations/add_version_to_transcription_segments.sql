-- Add version column to transcription_segments table
ALTER TABLE transcription_segments
ADD COLUMN version INT NOT NULL DEFAULT 1;

-- Create an index on the version column for better performance
CREATE INDEX idx_transcription_segments_version ON transcription_segments(version);

-- Update existing rows to set version to 1
UPDATE transcription_segments
SET version = 1;
