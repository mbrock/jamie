-- Add version column to transcription_segments table
ALTER TABLE transcription_segments
ADD COLUMN version INT NOT NULL DEFAULT 1;

-- Remove the trigger for word_alternatives table
DROP TRIGGER IF EXISTS word_alternative_changed ON word_alternatives;

-- Remove the function used by the trigger
DROP FUNCTION IF EXISTS notify_word_alternative_change();

-- Create an index on the version column for better performance
CREATE INDEX idx_transcription_segments_version ON transcription_segments(version);

-- Update transcription_segments table to set version to max version of words
UPDATE transcription_segments ts
SET version = COALESCE(
        (
            SELECT MAX(tw.version)
            FROM transcription_words tw
            WHERE tw.segment_id = ts.id
        ),
        1
    );