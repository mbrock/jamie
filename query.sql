-- name: CreateStream :exec
INSERT INTO streams (id, packet_seq_offset, sample_idx_offset)
VALUES (?, ?, ?);

-- name: SavePacket :exec
INSERT INTO packets (id, stream, packet_seq, sample_idx, payload)
VALUES (?, ?, ?, ?, ?);

-- name: CreateSpeaker :exec
INSERT INTO speakers (id, stream, emoji)
VALUES (?, ?, ?);

-- name: CreateDiscordSpeaker :exec
INSERT INTO discord_speakers (id, speaker, discord_id, ssrc, username)
VALUES (?, ?, ?, ?, ?);

-- name: CreateDiscordChannelStream :exec
INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel)
VALUES (?, ?, ?, ?);

-- name: CreateAttribution :exec
INSERT INTO attributions (id, stream, speaker)
VALUES (?, ?, ?);

-- name: SaveRecognition :exec
INSERT INTO recognitions (id, stream, sample_idx, sample_len, text, confidence)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetRecentTranscriptions :many
WITH ranked_recognitions AS (
    SELECT 
        ds.username,
        r.text,
        r.created_at,
        LAG(r.created_at, 1) OVER (ORDER BY r.created_at) AS prev_created_at,
        LAG(ds.username, 1) OVER (ORDER BY r.created_at) AS prev_username,
        ROW_NUMBER() OVER (ORDER BY r.created_at DESC) AS row_num
    FROM recognitions r
    JOIN speakers s ON r.stream = s.stream
    JOIN discord_speakers ds ON s.id = ds.speaker
),
grouped_recognitions AS (
    SELECT 
        username,
        text,
        created_at,
        CASE 
            WHEN prev_created_at IS NULL OR 
                 (JULIANDAY(created_at) - JULIANDAY(prev_created_at)) * 24 * 60 > 3 OR
                 username != prev_username
            THEN row_num 
            ELSE NULL 
        END AS group_start,
        MAX(CASE 
            WHEN prev_created_at IS NULL OR 
                 (JULIANDAY(created_at) - JULIANDAY(prev_created_at)) * 24 * 60 > 3 OR
                 username != prev_username
            THEN row_num 
            ELSE NULL 
        END) OVER (ORDER BY created_at ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS group_id
    FROM ranked_recognitions
)
SELECT 
    username,
    GROUP_CONCAT(text, ' ') AS text,
    MIN(created_at) AS created_at
FROM grouped_recognitions AS gr
GROUP BY group_id
ORDER BY created_at DESC;

-- name: GetTranscriptionsForTimeRange :many
WITH ranked_recognitions AS (
    SELECT s.emoji, r.text, r.created_at, r.stream, r.sample_idx,
           COALESCE(
               LEAD(r.created_at) OVER (PARTITION BY r.stream ORDER BY r.created_at), 
               julianday(datetime(?, '+10 seconds'))
           ) AS next_created_at
    FROM recognitions r
    JOIN speakers s ON r.stream = s.stream
    WHERE r.created_at >= julianday(?)
    ORDER BY r.created_at ASC
)
SELECT emoji, text, created_at, stream, sample_idx, next_created_at
FROM ranked_recognitions
WHERE created_at <= julianday(?) OR (next_created_at IS NULL AND created_at <= julianday(datetime(?, '+10 seconds')))
ORDER BY created_at ASC;

-- name: GetRecentStreamsWithTranscriptionCount :many
SELECT s.id, s.created_at, COUNT(r.id) as transcription_count
FROM streams s
LEFT JOIN discord_channel_streams dcs ON s.id = dcs.stream
LEFT JOIN recognitions r ON s.id = r.stream
WHERE (dcs.discord_guild = ? OR ? = '') AND (dcs.discord_channel = ? OR ? = '')
GROUP BY s.id
ORDER BY s.created_at DESC
LIMIT ?;

-- name: GetTranscriptionsForStream :many
SELECT s.emoji, r.text, r.created_at, r.sample_idx, r.sample_len, r.stream,
       COALESCE(LEAD(r.created_at) OVER (ORDER BY r.created_at), julianday('now')) AS end_time
FROM recognitions r
JOIN speakers s ON r.stream = s.stream
WHERE r.stream = ?
ORDER BY r.sample_idx ASC;

-- name: GetStreamForDiscordChannelAndSpeaker :one
SELECT s.id 
FROM streams s
JOIN discord_channel_streams dcs ON s.id = dcs.stream
JOIN speakers spk ON s.id = spk.stream
JOIN discord_speakers ds ON spk.id = ds.speaker
WHERE dcs.discord_guild = ? AND dcs.discord_channel = ? AND ds.discord_id = ?
ORDER BY s.created_at DESC
LIMIT 1;

-- name: CreateStreamForDiscordChannel :exec
INSERT INTO streams (id, packet_seq_offset, sample_idx_offset)
VALUES (?, ?, ?);

-- name: SaveSpeechRecognitionSession :exec
INSERT INTO speech_recognition_sessions (stream, session_data)
VALUES (?, ?);

-- name: GetSpeechRecognitionSession :one
SELECT session_data FROM speech_recognition_sessions WHERE stream = ?;

-- name: GetChannelAndUsernameForStream :one
SELECT dcs.discord_channel, ds.username
FROM discord_channel_streams dcs
JOIN streams st ON dcs.stream = st.id
JOIN speakers s ON st.id = s.stream
JOIN discord_speakers ds ON s.id = ds.speaker
WHERE st.id = ?;

-- name: UpdateSpeakerEmoji :exec
UPDATE speakers SET emoji = ? WHERE stream = ?;

-- name: GetChannelIDForStream :one
SELECT discord_channel FROM discord_channel_streams WHERE stream = ?;

-- name: EndStreamForChannel :exec
UPDATE streams
SET ended_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT stream
    FROM discord_channel_streams
    WHERE discord_guild = ? AND discord_channel = ?
) AND ended_at IS NULL;

-- name: GetTodayTranscriptions :many
SELECT s.emoji, r.text, r.created_at
FROM recognitions r
JOIN speakers s ON r.stream = s.stream
WHERE DATE(r.created_at) = DATE('now')
ORDER BY r.created_at ASC;

-- name: GetTranscriptionsForDuration :many
SELECT s.emoji, r.text, r.created_at
FROM recognitions r
JOIN speakers s ON r.stream = s.stream
WHERE r.created_at >= datetime('now', ?)
ORDER BY r.created_at ASC;

-- name: SetSystemPrompt :exec
INSERT OR REPLACE INTO system_prompts (name, prompt)
VALUES (?, ?);

-- name: GetSystemPrompt :one
SELECT prompt FROM system_prompts WHERE name = ?;

-- name: ListSystemPrompts :many
SELECT name, prompt FROM system_prompts;

-- name: GetPacketsForStreamInSampleRange :many
SELECT payload, sample_idx
FROM packets
WHERE stream = ? AND sample_idx >= ? AND sample_idx <= ?
ORDER BY sample_idx ASC;

-- name: SaveTextMessage :exec
INSERT INTO text_messages (id, discord_channel, discord_user, discord_message_id, content, is_bot)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetRecentTextMessages :many
SELECT id, discord_channel, discord_user, content, is_bot, created_at
FROM text_messages
WHERE discord_channel = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: GetTextMessagesInTimeRange :many
SELECT id, discord_channel, discord_user, content, is_bot, created_at
FROM text_messages
WHERE discord_channel = ? AND created_at BETWEEN ? AND ?
ORDER BY created_at ASC;

-- name: UpsertVoiceState :exec
INSERT INTO voice_states (id, ssrc, user_id, is_speaking, updated_at)
VALUES (?, ?, ?, ?, julianday('now'))
ON CONFLICT (id) DO UPDATE SET
    ssrc = excluded.ssrc,
    user_id = excluded.user_id,
    is_speaking = excluded.is_speaking,
    updated_at = julianday('now');

-- name: GetVoiceState :one
SELECT * FROM voice_states WHERE ssrc = ? OR user_id = ?;

-- name: UpdateDiscordSpeakerUsername :exec
UPDATE discord_speakers
SET username = ?
WHERE discord_id = ?;

-- name: GetRecentRecognitions :many
SELECT s.emoji, r.text, r.created_at, ds.username as discord_username, r.stream, r.sample_idx, r.sample_len
FROM recognitions r
JOIN speakers s ON r.stream = s.stream
JOIN discord_speakers ds ON s.id = ds.speaker
ORDER BY r.created_at DESC
LIMIT ?;

-- name: GetStream :one
SELECT * FROM streams WHERE id = ?;

-- name: GetAllStreamsWithDetails :many
SELECT 
    s.id,
    s.created_at,
    s.ended_at,
    dcs.discord_channel,
    ds.username,
    CAST(COALESCE(r.duration, 0) AS INTEGER) as duration,
    COALESCE(r.transcription_count, 0) as transcription_count
FROM 
    streams s
LEFT JOIN 
    discord_channel_streams dcs ON s.id = dcs.stream
LEFT JOIN 
    speakers sp ON s.id = sp.stream
LEFT JOIN 
    discord_speakers ds ON sp.id = ds.speaker
LEFT JOIN (
    SELECT 
        stream,
        COUNT(*) as transcription_count,
        (MAX(sample_idx) - MIN(sample_idx)) as duration
    FROM 
        recognitions
    GROUP BY 
        stream
) r ON s.id = r.stream
ORDER BY 
    s.created_at DESC;
