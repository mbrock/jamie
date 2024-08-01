CREATE TABLE IF NOT EXISTS discord_sessions (
    id SERIAL PRIMARY KEY,
    bot_token TEXT,
    user_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssrc_mappings (
    id SERIAL PRIMARY KEY,
    guild_id TEXT,
    channel_id TEXT,
    user_id TEXT,
    ssrc BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    session_id INTEGER REFERENCES discord_sessions(id)
);

CREATE TABLE IF NOT EXISTS opus_packets (
    id SERIAL PRIMARY KEY,
    guild_id TEXT,
    channel_id TEXT,
    ssrc BIGINT,
    sequence INTEGER,
    timestamp BIGINT,
    opus_data BYTEA,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    session_id INTEGER REFERENCES discord_sessions(id)
);

CREATE TABLE IF NOT EXISTS voice_state_events (
    id SERIAL PRIMARY KEY,
    guild_id TEXT,
    channel_id TEXT,
    user_id TEXT,
    session_id INTEGER REFERENCES discord_sessions(id),
    deaf BOOLEAN,
    mute BOOLEAN,
    self_deaf BOOLEAN,
    self_mute BOOLEAN,
    self_stream BOOLEAN,
    self_video BOOLEAN,
    suppress BOOLEAN,
    request_to_speak_timestamp TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);