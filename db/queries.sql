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
SELECT upsert_transcription_segment ($1, $2, $3, $4) AS id;

-- name: InsertTranscriptionWord :one
INSERT INTO transcription_words (segment_id, start_time, duration, is_eos)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: InsertWordAlternative :exec
INSERT INTO word_alternatives (word_id, content, confidence)
VALUES ($1, $2, $3);
