-- name: UpsertBotVoiceJoin :exec
INSERT INTO bot_voice_joins (guild_id, channel_id, session_id)
VALUES ($1, $2, $3)
ON CONFLICT (guild_id, session_id)
DO UPDATE SET channel_id = EXCLUDED.channel_id, joined_at = CURRENT_TIMESTAMP;

-- name: UpsertSSRCMapping :exec
INSERT INTO ssrc_mappings (guild_id, channel_id, user_id, ssrc, session_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (guild_id, channel_id, user_id, ssrc)
DO UPDATE SET session_id = EXCLUDED.session_id;

-- name: InsertOpusPacket :exec
INSERT INTO opus_packets (guild_id, channel_id, ssrc, sequence, timestamp, opus_data, session_id)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertDiscordSession :one
INSERT INTO discord_sessions (bot_token, user_id)
VALUES ($1, $2)
RETURNING id;

-- name: GetLastJoinedChannel :one
SELECT channel_id
FROM bot_voice_joins
WHERE guild_id = $1 AND session_id = (
    SELECT id
    FROM discord_sessions
    WHERE bot_token = $2
    ORDER BY created_at DESC
    LIMIT 1
)
ORDER BY joined_at DESC
LIMIT 1;

-- name: InsertVoiceStateEvent :exec
INSERT INTO voice_state_events (
    guild_id, channel_id, user_id, session_id,
    deaf, mute, self_deaf, self_mute,
    self_stream, self_video, suppress,
    request_to_speak_timestamp
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
);

-- name: GetOpusPackets :many
SELECT id, sequence, timestamp, created_at, opus_data
FROM opus_packets
WHERE ssrc = $1 AND created_at BETWEEN $2 AND $3
ORDER BY created_at;
