-- Convert timestamp columns to timestamptz and interpret as EEST

-- discord_sessions table
ALTER TABLE discord_sessions
ALTER COLUMN created_at TYPE timestamptz
USING created_at AT TIME ZONE 'EEST';

-- ssrc_mappings table
ALTER TABLE ssrc_mappings
ALTER COLUMN created_at TYPE timestamptz
USING created_at AT TIME ZONE 'EEST';

-- opus_packets table
ALTER TABLE opus_packets
ALTER COLUMN created_at TYPE timestamptz
USING created_at AT TIME ZONE 'EEST';

-- voice_state_events table
ALTER TABLE voice_state_events
ALTER COLUMN request_to_speak_timestamp TYPE timestamptz
USING request_to_speak_timestamp AT TIME ZONE 'EEST';

ALTER TABLE voice_state_events
ALTER COLUMN created_at TYPE timestamptz
USING created_at AT TIME ZONE 'EEST';

-- bot_voice_joins table
ALTER TABLE bot_voice_joins
ALTER COLUMN joined_at TYPE timestamptz
USING joined_at AT TIME ZONE 'EEST';
