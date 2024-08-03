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
    id BIGSERIAL PRIMARY KEY,
    ssrc BIGINT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transcription_segments (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES transcription_sessions(id),
    is_final BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transcription_words (
    id BIGSERIAL PRIMARY KEY,
    segment_id BIGINT NOT NULL REFERENCES transcription_segments(id),
    start_time INTERVAL NOT NULL,
    duration INTERVAL NOT NULL,
    is_eos BOOLEAN NOT NULL,
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS word_alternatives (
    id BIGSERIAL PRIMARY KEY,
    word_id BIGINT NOT NULL REFERENCES transcription_words(id),
    content TEXT NOT NULL,
    confidence FLOAT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Create a function to notify about transcription changes
CREATE OR REPLACE FUNCTION notify_transcription_change() RETURNS TRIGGER AS $$
DECLARE
    segment_id BIGINT;
    session_id BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'transcription_segments' THEN
        segment_id := NEW.id;
        session_id := NEW.session_id;
    ELSIF TG_TABLE_NAME = 'word_alternatives' THEN
        SELECT tw.segment_id, ts.session_id
        INTO segment_id, session_id
        FROM transcription_words tw
        JOIN transcription_segments ts ON tw.segment_id = ts.id
        WHERE tw.id = NEW.word_id;
    END IF;

    PERFORM pg_notify('transcription_change', json_build_object(
        'operation', TG_OP,
        'id', segment_id,
        'session_id', session_id
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger for transcription_segments table
CREATE TRIGGER transcription_segment_changed
AFTER INSERT OR UPDATE ON transcription_segments
FOR EACH ROW EXECUTE FUNCTION notify_transcription_change();

-- Create a trigger for word_alternatives table
CREATE TRIGGER word_alternative_changed
AFTER INSERT OR UPDATE ON word_alternatives
FOR EACH ROW EXECUTE FUNCTION notify_transcription_change();

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_transcription_segments_session_id ON transcription_segments(session_id);
CREATE INDEX IF NOT EXISTS idx_transcription_words_segment_id ON transcription_words(segment_id);
CREATE INDEX IF NOT EXISTS idx_word_alternatives_word_id ON word_alternatives(word_id);

-- Function to upsert transcription segment
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'upsert_transcription_segment') THEN
        DROP FUNCTION upsert_transcription_segment;
    END IF;
END
$$;

CREATE FUNCTION upsert_transcription_segment(
        p_session_id BIGINT,
        p_is_final BOOLEAN
    ) RETURNS TABLE (segment_id BIGINT, version INT) AS $$
DECLARE 
    v_segment_id BIGINT;
    v_current_version INT;
BEGIN
    -- Check if the last segment is final
    SELECT id INTO v_segment_id
    FROM transcription_segments
    WHERE session_id = p_session_id
    ORDER BY id DESC
    LIMIT 1;

    IF v_segment_id IS NULL OR (SELECT is_final FROM transcription_segments WHERE id = v_segment_id) THEN
        -- Insert a new segment
        INSERT INTO transcription_segments (session_id, is_final)
        VALUES (p_session_id, p_is_final)
        RETURNING id INTO v_segment_id;
        v_current_version := 1;
    ELSE
        -- Update the existing segment
        UPDATE transcription_segments
        SET is_final = p_is_final
        WHERE id = v_segment_id;

        -- Get the current max version for this segment
        SELECT COALESCE(MAX(version), 0) + 1 INTO v_current_version
        FROM transcription_words
        WHERE segment_id = v_segment_id;

        -- If it's not a final segment, we don't delete existing words, just add new ones with a new version
    END IF;

    RETURN QUERY SELECT v_segment_id AS segment_id, v_current_version AS version;
END;
$$ LANGUAGE plpgsql;
