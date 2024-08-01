CREATE TABLE IF NOT EXISTS voice_sessions (
    id TEXT PRIMARY KEY,
    discord_guild_id TEXT NOT NULL,
    discord_channel_id TEXT NOT NULL,
    started_at REAL NOT NULL DEFAULT (julianday('now')),
    ended_at REAL
);

CREATE TABLE IF NOT EXISTS voice_streams (
    id TEXT PRIMARY KEY,
    voice_session_id TEXT NOT NULL,
    ssrc INTEGER NOT NULL,
    discord_user_id TEXT NOT NULL,
    FOREIGN KEY (voice_session_id) REFERENCES voice_sessions(id)
);

CREATE TABLE IF NOT EXISTS voice_packets (
    id TEXT PRIMARY KEY,
    voice_stream_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    sample_idx INTEGER NOT NULL,
    payload BLOB NOT NULL,
    received_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (voice_stream_id) REFERENCES voice_streams(id)
);

CREATE TABLE IF NOT EXISTS recognition_sessions (
    id TEXT PRIMARY KEY,
    voice_session_id TEXT NOT NULL,
    first_sample_idx INTEGER NOT NULL,
    FOREIGN KEY (voice_session_id) REFERENCES voice_sessions(id)
);

CREATE TABLE IF NOT EXISTS recognition_results (
    id TEXT PRIMARY KEY,
    recognition_session_id TEXT NOT NULL,
    start_second REAL NOT NULL,
    end_second REAL NOT NULL,
    text TEXT NOT NULL,
    confidence REAL NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (recognition_session_id) REFERENCES recognition_sessions(id)
);

CREATE TABLE IF NOT EXISTS text_messages (
    id TEXT PRIMARY KEY,
    discord_guild_id TEXT NOT NULL,
    discord_channel_id TEXT NOT NULL,
    discord_user_id TEXT NOT NULL,
    discord_message_id TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now'))
);

CREATE TABLE IF NOT EXISTS system_prompts (
    name TEXT PRIMARY KEY,
    prompt TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    updated_at REAL NOT NULL DEFAULT (julianday('now'))
);