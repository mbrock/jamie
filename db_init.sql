CREATE TABLE IF NOT EXISTS ssrc_mappings (
    guild_id TEXT,
    channel_id TEXT,
    user_id TEXT,
    ssrc BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (guild_id, channel_id, ssrc)
);

CREATE TABLE IF NOT EXISTS opus_packets (
    id SERIAL PRIMARY KEY,
    guild_id TEXT,
    channel_id TEXT,
    ssrc BIGINT,
    sequence INTEGER,
    timestamp BIGINT,
    opus_data BYTEA,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
