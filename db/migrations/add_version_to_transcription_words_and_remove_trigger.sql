-- Add version column to transcription_words table
ALTER TABLE transcription_words
ADD COLUMN version INT NOT NULL DEFAULT 1;

-- Create an index on the version column for better performance
CREATE INDEX idx_transcription_words_version ON transcription_words(version);

-- Remove the trigger for word_alternatives table
DROP TRIGGER IF EXISTS word_alternative_changed ON word_alternatives;

-- Remove the function used by the trigger
DROP FUNCTION IF EXISTS notify_word_alternative_change();
