-- Add version column to transcription_words table
ALTER TABLE transcription_words
ADD COLUMN version INT NOT NULL DEFAULT 1;

-- Remove the trigger for word_alternatives table
DROP TRIGGER IF EXISTS word_alternative_changed ON word_alternatives;

-- Remove the function used by the trigger
DROP FUNCTION IF EXISTS notify_word_alternative_change();

-- Update transcription_segments table to set version to max version of words
UPDATE transcription_segments ts
SET version = COALESCE(
    (SELECT MAX(tw.version)
     FROM transcription_words tw
     WHERE tw.segment_id = ts.id),
    1
);

-- Ensure that the transcription_segments version is always at least as high as its words
UPDATE transcription_segments ts
SET version = GREATEST(ts.version, 
    COALESCE((SELECT MAX(tw.version)
              FROM transcription_words tw
              WHERE tw.segment_id = ts.id),
             1));
