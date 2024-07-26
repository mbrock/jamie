CREATE TABLE IF NOT EXISTS streams (
    id TEXT PRIMARY KEY,
    packet_seq_offset INTEGER NOT NULL,
    sample_idx_offset INTEGER NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    ended_at REAL
);

CREATE TABLE IF NOT EXISTS packets (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    packet_seq INTEGER NOT NULL,
    sample_idx INTEGER NOT NULL,
    payload BLOB NOT NULL,
    received_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS speakers (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    emoji TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS discord_speakers (
    id TEXT PRIMARY KEY,
    speaker TEXT NOT NULL,
    discord_id TEXT NOT NULL,
    ssrc INTEGER NOT NULL,
    username TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (speaker) REFERENCES speakers(id)
);

CREATE TABLE IF NOT EXISTS discord_channel_streams (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    discord_guild TEXT NOT NULL,
    discord_channel TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS attributions (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    speaker TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (stream) REFERENCES streams(id),
    FOREIGN KEY (speaker) REFERENCES speakers(id)
);

CREATE TABLE IF NOT EXISTS recognitions (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    sample_idx INTEGER NOT NULL,
    sample_len INTEGER NOT NULL,
    text TEXT NOT NULL,
    confidence REAL NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS speech_recognition_sessions (
    stream TEXT PRIMARY KEY,
    session_data TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS system_prompts (
    name TEXT PRIMARY KEY,
    prompt TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    updated_at REAL NOT NULL DEFAULT (julianday('now'))
);

CREATE TABLE IF NOT EXISTS text_messages (
    id TEXT PRIMARY KEY,
    discord_channel TEXT NOT NULL,
    discord_user TEXT NOT NULL,
    discord_message_id TEXT NOT NULL,
    content TEXT NOT NULL,
    is_bot BOOLEAN NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now'))
);

CREATE TABLE IF NOT EXISTS voice_states (
    id TEXT PRIMARY KEY,
    ssrc INTEGER NOT NULL,
    user_id TEXT NOT NULL,
    is_speaking BOOLEAN NOT NULL,
    updated_at REAL NOT NULL DEFAULT (julianday('now'))
);
