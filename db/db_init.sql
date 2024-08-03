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

CREATE TABLE IF NOT EXISTS transcription_sessions (
    id SERIAL PRIMARY KEY,
    ssrc BIGINT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transcription_segments (
    id SERIAL PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES transcription_sessions(id),
    is_final BOOLEAN NOT NULL,
    start_offset INTEGER NOT NULL,
    end_offset INTEGER NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transcription_words (
    id SERIAL PRIMARY KEY,
    segment_id INTEGER NOT NULL REFERENCES transcription_segments(id),
    start_time INTERVAL NOT NULL,
    duration INTERVAL NOT NULL,
    is_eos BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS word_alternatives (
    id SERIAL PRIMARY KEY,
    word_id INTEGER NOT NULL REFERENCES transcription_words(id),
    content TEXT NOT NULL,
    confidence FLOAT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Function to upsert transcription segment
CREATE OR REPLACE FUNCTION upsert_transcription_segment(
    p_session_id BIGINT,
    p_is_final BOOLEAN,
    p_start_offset INT,
    p_end_offset INT
) RETURNS BIGINT AS $$
DECLARE
    v_segment_id BIGINT;
BEGIN
    -- Check if the last segment is final
    SELECT id INTO v_segment_id
    FROM transcription_segments
    WHERE session_id = p_session_id
    ORDER BY id DESC
    LIMIT 1;

    IF v_segment_id IS NULL OR (SELECT is_final FROM transcription_segments WHERE id = v_segment_id) THEN
        -- Insert a new segment
        INSERT INTO transcription_segments (session_id, is_final, start_offset, end_offset)
        VALUES (p_session_id, p_is_final, p_start_offset, p_end_offset)
        RETURNING id INTO v_segment_id;
    ELSE
        -- Update the existing segment
        UPDATE transcription_segments
        SET end_offset = p_end_offset,
            is_final = p_is_final
        WHERE id = v_segment_id;
    END IF;

    RETURN v_segment_id;
END;
$$ LANGUAGE plpgsql;
