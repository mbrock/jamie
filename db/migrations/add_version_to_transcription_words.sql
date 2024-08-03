-- Add version column to transcription_words table
ALTER TABLE transcription_words
ADD COLUMN version INT NOT NULL DEFAULT 1;

-- Create an index on the version column for better performance
CREATE INDEX idx_transcription_words_version ON transcription_words(version);
