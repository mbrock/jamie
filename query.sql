-- name: GetRecognitionResultsInTimeRange :many
SELECT r.id, rs.voice_session_id, vs.ssrc, vs.first_sample_idx + (r.start_second * 48000) AS sample_idx,
       r.start_second, r.end_second, r.text, r.confidence, r.created_at,
       vs.user_id
FROM recognition_results r
JOIN recognition_sessions rs ON r.recognition_session_id = rs.id
JOIN voice_streams vs ON rs.voice_session_id = vs.session_id
WHERE rs.voice_session_id = ?
  AND r.created_at >= ? AND r.created_at <= ?
ORDER BY r.created_at ASC;

-- name: GetTextMessagesInTimeRange :many
SELECT id, discord_guild_id, discord_channel_id, discord_user_id, discord_message_id, content, created_at
FROM text_messages
WHERE discord_channel_id = ?
  AND created_at >= ? AND created_at <= ?
ORDER BY created_at ASC;

-- name: GetAudioPacketsInTimeRange :many
SELECT id, voice_stream_id, sequence, sample_idx, payload, received_at
FROM voice_packets
WHERE voice_stream_id = ?
  AND received_at >= ? AND received_at <= ?
ORDER BY sample_idx ASC;

-- name: GetVoiceSessionInfo :one
SELECT id, guild_id, channel_id, started_at, ended_at
FROM voice_sessions
WHERE id = ?;

-- name: GetCurrentVoiceStreams :many
SELECT id, session_id, ssrc, user_id
FROM voice_streams
WHERE session_id = ?;

-- name: CreateVoiceSession :exec
INSERT INTO voice_sessions (id, guild_id, channel_id)
VALUES (?, ?, ?);

-- name: EndVoiceSession :exec
UPDATE voice_sessions
SET ended_at = CURRENT_TIMESTAMP
WHERE id = ? AND ended_at IS NULL;

-- name: CreateVoiceStream :exec
INSERT INTO voice_streams (id, session_id, ssrc, user_id)
VALUES (?, ?, ?, ?);

-- name: InsertVoicePacket :exec
INSERT INTO voice_packets (id, voice_stream_id, sequence, sample_idx, payload)
VALUES (?, ?, ?, ?, ?);

-- name: CreateRecognitionSession :exec
INSERT INTO recognition_sessions (id, voice_session_id, first_sample_idx)
VALUES (?, ?, ?);

-- name: InsertRecognitionResult :exec
INSERT INTO recognition_results (id, recognition_session_id, start_second, end_second, text, confidence)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetLatestVoiceSession :one
SELECT id, guild_id, channel_id, started_at, ended_at
FROM voice_sessions
WHERE guild_id = ? AND channel_id = ?
ORDER BY started_at DESC
LIMIT 1;

-- name: SaveTextMessage :exec
INSERT INTO text_messages (id, discord_guild_id, discord_channel_id, discord_user_id, discord_message_id, content)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSystemPrompt :one
SELECT prompt FROM system_prompts WHERE name = ?;

-- name: SetSystemPrompt :exec
INSERT OR REPLACE INTO system_prompts (name, prompt)
VALUES (?, ?);

-- name: ListSystemPrompts :many
SELECT name, prompt FROM system_prompts;
