CREATE TABLE IF NOT EXISTS transcripts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id TEXT,
    channel_id TEXT,
    transcript TEXT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS discord_voice_packet (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    stream_id TEXT,
    packet BLOB,
    relative_sequence INTEGER,
    relative_opus_timestamp INTEGER,
    receive_time INTEGER,
    FOREIGN KEY (stream_id) REFERENCES discord_voice_stream(stream_id)
);

CREATE TABLE IF NOT EXISTS discord_voice_stream (
    stream_id TEXT PRIMARY KEY,
    guild_id TEXT,
    channel_id TEXT,
    ssrc INTEGER,
    user_id TEXT,
    first_opus_timestamp INTEGER,
    first_receive_time INTEGER,
    first_sequence INTEGER
);