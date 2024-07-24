CREATE TABLE IF NOT EXISTS streams (
    id TEXT PRIMARY KEY,
    packet_seq_offset INTEGER,
    sample_idx_offset INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME
);

CREATE TABLE IF NOT EXISTS packets (
    id TEXT PRIMARY KEY,
    stream TEXT,
    packet_seq INTEGER,
    sample_idx INTEGER,
    payload BLOB,
    received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS speakers (
    id TEXT PRIMARY KEY,
    stream TEXT,
    emoji TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS discord_speakers (
    id TEXT PRIMARY KEY,
    speaker TEXT,
    discord_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (speaker) REFERENCES speakers(id)
);

CREATE TABLE IF NOT EXISTS discord_channel_streams (
    id TEXT PRIMARY KEY,
    stream TEXT,
    discord_guild TEXT,
    discord_channel TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS attributions (
    id TEXT PRIMARY KEY,
    stream TEXT,
    speaker TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stream) REFERENCES streams(id),
    FOREIGN KEY (speaker) REFERENCES speakers(id)
);

CREATE TABLE IF NOT EXISTS recognitions (
    id TEXT PRIMARY KEY,
    stream TEXT,
    sample_idx INTEGER,
    sample_len INTEGER,
    text TEXT,
    confidence REAL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stream) REFERENCES streams(id)
);

CREATE TABLE IF NOT EXISTS speech_recognition_sessions (
    stream TEXT PRIMARY KEY,
    session_data TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (stream) REFERENCES streams(id)
);
