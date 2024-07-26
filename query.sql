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
INSERT INTO discord_speakers (id, speaker, discord_id)
VALUES (?, ?, ?);

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
        s.emoji,
        r.text,
        r.created_at,
        LAG(r.created_at, 1) OVER (ORDER BY r.created_at) AS prev_created_at,
        LAG(s.emoji, 1) OVER (ORDER BY r.created_at) AS prev_emoji,
        ROW_NUMBER() OVER (ORDER BY r.created_at DESC) AS row_num
    FROM recognitions r
    JOIN speakers s ON r.stream = s.stream
),
grouped_recognitions AS (
    SELECT 
        emoji,
        text,
        created_at,
        CASE 
            WHEN prev_created_at IS NULL OR 
                 (JULIANDAY(created_at) - JULIANDAY(prev_created_at)) * 24 * 60 > 3 OR
                 emoji != prev_emoji
            THEN row_num 
            ELSE NULL 
        END AS group_start,
        MAX(CASE 
            WHEN prev_created_at IS NULL OR 
                 (JULIANDAY(created_at) - JULIANDAY(prev_created_at)) * 24 * 60 > 3 OR
                 emoji != prev_emoji
            THEN row_num 
            ELSE NULL 
        END) OVER (ORDER BY created_at ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS group_id
    FROM ranked_recognitions
)
SELECT 
    emoji,
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
SELECT s.emoji, r.text, r.created_at, r.sample_idx, r.stream,
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

-- name: GetChannelAndEmojiForStream :one
SELECT dcs.discord_channel, s.emoji 
FROM discord_channel_streams dcs
JOIN streams st ON dcs.stream = st.id
JOIN speakers s ON st.id = s.stream
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
WHERE stream = ? AND sample_idx BETWEEN ? AND ?
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
