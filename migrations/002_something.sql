-- Add unique constraint to ssrc_mappings table
ALTER TABLE ssrc_mappings
ADD CONSTRAINT unique_ssrc_mapping
UNIQUE (guild_id, channel_id, user_id, ssrc);
