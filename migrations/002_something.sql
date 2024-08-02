-- Create transcriptions table
CREATE TABLE IF NOT EXISTS transcriptions (
    id SERIAL PRIMARY KEY,
    opus_packet_id INTEGER NOT NULL REFERENCES opus_packets(id),
    transcription_service TEXT NOT NULL,
    transcription_text TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Create index on opus_packet_id for faster lookups
CREATE INDEX IF NOT EXISTS idx_transcriptions_opus_packet_id ON transcriptions (opus_packet_id);

-- Add a column to opus_packets to indicate if it has been transcribed
ALTER TABLE opus_packets ADD COLUMN IF NOT EXISTS transcribed BOOLEAN DEFAULT FALSE;

-- Create a function to update the transcribed status of opus_packets
CREATE OR REPLACE FUNCTION update_opus_packet_transcribed_status() RETURNS TRIGGER AS $$
BEGIN
    UPDATE opus_packets SET transcribed = TRUE WHERE id = NEW.opus_packet_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger to automatically update the transcribed status
CREATE TRIGGER update_opus_packet_transcribed_status_trigger
AFTER INSERT ON transcriptions
FOR EACH ROW
EXECUTE FUNCTION update_opus_packet_transcribed_status();
