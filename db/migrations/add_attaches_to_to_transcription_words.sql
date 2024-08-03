-- Add attaches_to column to transcription_words table
ALTER TABLE transcription_words
ADD COLUMN attaches_to TEXT;

-- Create an index on the attaches_to column for better performance
CREATE INDEX idx_transcription_words_attaches_to ON transcription_words(attaches_to);
