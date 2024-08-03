-- name: UpsertBotVoiceJoin :exec
INSERT INTO bot_voice_joins (guild_id, channel_id, session_id)
VALUES ($1, $2, $3) ON CONFLICT (guild_id, session_id) DO
UPDATE
SET channel_id = EXCLUDED.channel_id,
    joined_at = CURRENT_TIMESTAMP;

-- name: GetVoiceActivityReport :many
SELECT u.user_id,
    COUNT(DISTINCT op.id) AS packet_count,
    MIN(op.created_at)::TIMESTAMPTZ AS first_packet,
    MAX(op.created_at)::TIMESTAMPTZ AS last_packet,
    SUM(LENGTH(op.opus_data)) AS total_bytes
FROM opus_packets op
    JOIN ssrc_mappings u ON op.ssrc = u.ssrc
WHERE op.created_at BETWEEN $1 AND $2
GROUP BY u.user_id
ORDER BY packet_count DESC;

-- name: UpsertSSRCMapping :exec
INSERT INTO ssrc_mappings (guild_id, channel_id, user_id, ssrc, session_id)
VALUES ($1, $2, $3, $4, $5) ON CONFLICT (guild_id, channel_id, user_id, ssrc) DO
UPDATE
SET session_id = EXCLUDED.session_id;

-- name: InsertOpusPacket :exec
INSERT INTO opus_packets (
        guild_id,
        channel_id,
        ssrc,
        sequence,
        timestamp,
        opus_data,
        session_id
    )
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertDiscordSession :one
INSERT INTO discord_sessions (bot_token, user_id)
VALUES ($1, $2)
RETURNING id;

-- name: GetLastJoinedChannel :one
SELECT bvj.channel_id
FROM bot_voice_joins bvj
    JOIN discord_sessions ds ON bvj.session_id = ds.id
WHERE bvj.guild_id = $1
    AND ds.bot_token = $2
ORDER BY bvj.joined_at DESC
LIMIT 1;

-- name: InsertVoiceStateEvent :exec
INSERT INTO voice_state_events (
        guild_id,
        channel_id,
        user_id,
        session_id,
        deaf,
        mute,
        self_deaf,
        self_mute,
        self_stream,
        self_video,
        suppress,
        request_to_speak_timestamp
    )
VALUES (
        $1,
        $2,
        $3,
        $4,
        $5,
        $6,
        $7,
        $8,
        $9,
        $10,
        $11,
        $12
    );

-- name: GetOpusPackets :many
SELECT *
FROM opus_packets
WHERE ssrc = $1
    AND created_at BETWEEN $2 AND $3
ORDER BY created_at;

-- name: GetUploadedFileByHash :one
SELECT remote_uri
FROM uploaded_files
WHERE hash = $1
LIMIT 1;

-- name: InsertUploadedFile :exec
INSERT INTO uploaded_files (hash, file_name, remote_uri)
VALUES ($1, $2, $3);

-- name: GetUserIDBySSRC :one
SELECT user_id
FROM ssrc_mappings
WHERE ssrc = $1
LIMIT 1;

-- name: InsertTranscriptionSession :one
INSERT INTO transcription_sessions (ssrc, start_time, guild_id, channel_id, user_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: UpsertTranscriptionSegment :one
SELECT upsert_transcription_segment (
        sqlc.arg(session_id),
        sqlc.arg(is_final)
    ) AS id;

-- name: InsertTranscriptionWord :one
INSERT INTO transcription_words (segment_id, start_time, duration, is_eos)
VALUES (
        sqlc.arg(segment_id),
        make_interval(secs => sqlc.arg(start_time)),
        make_interval(secs => sqlc.arg(duration)),
        sqlc.arg(is_eos)
    )
RETURNING id;

-- name: InsertWordAlternative :exec
INSERT INTO word_alternatives (word_id, content, confidence)
VALUES ($1, $2, $3);

-- name: GetAllFinalTranscripts :many
SELECT ts.id, ts.session_id, ts.is_final, tw.id as word_id, tw.start_time, tw.duration, tw.is_eos, wa.content, wa.confidence
FROM transcription_segments ts
JOIN transcription_words tw ON ts.id = tw.segment_id
JOIN word_alternatives wa ON tw.id = wa.word_id
WHERE ts.is_final = true
ORDER BY ts.id, tw.id, wa.confidence DESC;

-- name: GetLatestNonFinalTranscripts :many
SELECT DISTINCT ON (ts.session_id) 
    ts.id, ts.session_id, ts.is_final, tw.id as word_id, tw.start_time, tw.duration, tw.is_eos, wa.content, wa.confidence
FROM transcription_segments ts
JOIN transcription_words tw ON ts.id = tw.segment_id
JOIN word_alternatives wa ON tw.id = wa.word_id
WHERE ts.is_final = false
ORDER BY ts.session_id, ts.id DESC, tw.id, wa.confidence DESC;

-- name: GetTranscriptSegment :many
SELECT ts.id, ts.session_id, ts.is_final, tw.id as word_id, tw.start_time, tw.duration, tw.is_eos, wa.content, wa.confidence
FROM transcription_segments ts
JOIN transcription_words tw ON ts.id = tw.segment_id
JOIN word_alternatives wa ON tw.id = wa.word_id
WHERE ts.id = $1
ORDER BY tw.id, wa.confidence DESC;

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_transcription_segments_is_final ON transcription_segments(is_final);
CREATE INDEX IF NOT EXISTS idx_transcription_words_segment_id ON transcription_words(segment_id);
CREATE INDEX IF NOT EXISTS idx_word_alternatives_word_id ON word_alternatives(word_id);

-- Create a function to notify about transcription changes
CREATE OR REPLACE FUNCTION notify_transcription_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('transcription_change', json_build_object(
        'operation', TG_OP,
        'id', NEW.id,
        'session_id', NEW.session_id,
        'is_final', NEW.is_final
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger for transcription_segments table
CREATE TRIGGER transcription_segment_changed
AFTER INSERT OR UPDATE ON transcription_segments
FOR EACH ROW EXECUTE FUNCTION notify_transcription_change();
CREATE INDEX IF NOT EXISTS idx_word_alternatives_word_id ON word_alternatives(word_id);

-- Create a function to notify about transcription changes
CREATE OR REPLACE FUNCTION notify_transcription_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('transcription_change', json_build_object(
        'operation', TG_OP,
        'id', NEW.id,
        'session_id', NEW.session_id,
        'is_final', NEW.is_final
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger for transcription_segments table
CREATE TRIGGER transcription_segment_changed
AFTER INSERT OR UPDATE ON transcription_segments
FOR EACH ROW EXECUTE FUNCTION notify_transcription_change();

-- Create a function to notify about transcription changes
CREATE OR REPLACE FUNCTION notify_transcription_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('transcription_change', json_build_object(
        'operation', TG_OP,
        'id', NEW.id,
        'session_id', NEW.session_id,
        'is_final', NEW.is_final
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger for transcription_segments table
CREATE TRIGGER transcription_segment_changed
AFTER INSERT OR UPDATE ON transcription_segments
FOR EACH ROW EXECUTE FUNCTION notify_transcription_change();

-- Create a function to notify about transcription changes
CREATE OR REPLACE FUNCTION notify_transcription_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('transcription_change', json_build_object(
        'operation', TG_OP,
        'id', NEW.id,
        'session_id', NEW.session_id,
        'is_final', NEW.is_final
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger for transcription_segments table
CREATE TRIGGER transcription_segment_changed
AFTER INSERT OR UPDATE ON transcription_segments
FOR EACH ROW EXECUTE FUNCTION notify_transcription_change();
