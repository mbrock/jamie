-- name: GetRecognitionsInTimeRange :many
SELECT r.id, r.session_id, r.ssrc, r.sample_idx, r.sample_len, r.text, r.confidence, r.created_at,
       vs.user_id, vs.username
FROM recognitions r
JOIN voice_state_events vs ON r.session_id = vs.session_id AND r.ssrc = vs.ssrc
WHERE r.session_id = ?
  AND r.created_at BETWEEN ? AND ?
ORDER BY r.created_at ASC;

-- name: GetTextMessagesInTimeRange :many
SELECT id, discord_channel, discord_user, discord_message_id, content, is_bot, created_at
FROM text_messages
WHERE discord_channel = ?
  AND created_at BETWEEN ? AND ?
ORDER BY created_at ASC;

-- name: GetAudioPacketsInTimeRange :many
SELECT id, session_id, ssrc, packet_seq, sample_idx, payload, received_at
FROM voice_packets
WHERE session_id = ?
  AND received_at BETWEEN ? AND ?
ORDER BY sample_idx ASC;

-- name: GetVoiceSessionInfo :one
SELECT id, guild_id, channel_id, started_at, ended_at
FROM voice_sessions
WHERE id = ?;

-- name: GetCurrentVoiceState :many
SELECT session_id, user_id, username, ssrc, is_speaking, event_time
FROM current_voice_state
WHERE session_id = ?;

-- name: CreateVoiceSession :exec
INSERT INTO voice_sessions (id, guild_id, channel_id)
VALUES (?, ?, ?);

-- name: EndVoiceSession :exec
UPDATE voice_sessions
SET ended_at = CURRENT_TIMESTAMP
WHERE id = ? AND ended_at IS NULL;

-- name: InsertVoiceStateEvent :exec
INSERT INTO voice_state_events (id, session_id, user_id, username, ssrc, is_speaking)
VALUES (?, ?, ?, ?, ?, ?);

-- name: InsertVoicePacket :exec
INSERT INTO voice_packets (id, session_id, ssrc, packet_seq, sample_idx, payload)
VALUES (?, ?, ?, ?, ?, ?);

-- name: InsertRecognition :exec
INSERT INTO recognitions (id, session_id, ssrc, sample_idx, sample_len, text, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetLatestVoiceSession :one
SELECT id, guild_id, channel_id, started_at, ended_at
FROM voice_sessions
WHERE guild_id = ? AND channel_id = ?
ORDER BY started_at DESC
LIMIT 1;

-- name: SaveTextMessage :exec
INSERT INTO text_messages (id, discord_channel, discord_user, discord_message_id, content, is_bot)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSystemPrompt :one
SELECT prompt FROM system_prompts WHERE name = ?;

-- name: SetSystemPrompt :exec
INSERT OR REPLACE INTO system_prompts (name, prompt)
VALUES (?, ?);

-- name: ListSystemPrompts :many
SELECT name, prompt FROM system_prompts;
