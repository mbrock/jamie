CREATE TABLE IF NOT EXISTS discord_sessions (
    id SERIAL PRIMARY KEY,
    bot_token TEXT NOT NULL,
    user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssrc_mappings (
    id SERIAL PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    ssrc BIGINT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    session_id INTEGER NOT NULL REFERENCES discord_sessions(id),
    UNIQUE (guild_id, channel_id, user_id, ssrc)
);

CREATE TABLE IF NOT EXISTS opus_packets (
    id SERIAL PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    ssrc BIGINT NOT NULL,
    sequence INTEGER NOT NULL,
    timestamp BIGINT NOT NULL,
    opus_data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    session_id INTEGER NOT NULL REFERENCES discord_sessions(id)
);

CREATE TABLE IF NOT EXISTS voice_state_events (
    id SERIAL PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    session_id INTEGER NOT NULL REFERENCES discord_sessions(id),
    deaf BOOLEAN NOT NULL,
    mute BOOLEAN NOT NULL,
    self_deaf BOOLEAN NOT NULL,
    self_mute BOOLEAN NOT NULL,
    self_stream BOOLEAN NOT NULL,
    self_video BOOLEAN NOT NULL,
    suppress BOOLEAN NOT NULL,
    request_to_speak_timestamp TIMESTAMP,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bot_voice_joins (
    id SERIAL PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    session_id INTEGER REFERENCES discord_sessions(id),
    joined_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (guild_id, session_id)
);

-- Create a function to notify about new opus packets
CREATE OR REPLACE FUNCTION notify_new_opus_packet() RETURNS TRIGGER AS $$ BEGIN PERFORM pg_notify('new_opus_packet', row_to_json(NEW)::text);

RETURN NEW;

END;

$$ LANGUAGE plpgsql;

DO $$ BEGIN IF NOT EXISTS (
    SELECT 1
    FROM pg_trigger
    WHERE tgname = 'opus_packet_inserted'
) THEN CREATE TRIGGER opus_packet_inserted
AFTER
INSERT ON opus_packets FOR EACH ROW EXECUTE FUNCTION notify_new_opus_packet();

END IF;

END $$;

-- Create an index on the opus_packets table
CREATE INDEX IF NOT EXISTS idx_opus_packets_ssrc_created_at ON opus_packets (ssrc, created_at);

CREATE TABLE IF NOT EXISTS uploaded_files (
    id SERIAL PRIMARY KEY,
    hash TEXT UNIQUE NOT NULL,
    file_name TEXT NOT NULL,
    remote_uri TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
