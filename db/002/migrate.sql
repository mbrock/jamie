-- Migrate data from old tables to new tables

-- Create new tables
CREATE TABLE IF NOT EXISTS streams (
    id TEXT PRIMARY KEY,
    packet_seq_offset INTEGER,
    sample_idx_offset INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

-- Migrate data from discord_voice_stream to streams and discord_channel_streams
INSERT INTO streams (id, packet_seq_offset, sample_idx_offset, created_at)
SELECT stream_id, first_sequence, first_opus_timestamp, datetime(first_receive_time/1000000000, 'unixepoch')
FROM discord_voice_stream;

INSERT INTO discord_channel_streams (id, stream, discord_guild, discord_channel, created_at)
SELECT stream_id, stream_id, guild_id, channel_id, datetime(first_receive_time/1000000000, 'unixepoch')
FROM discord_voice_stream;

-- Migrate data from discord_voice_packet to packets
INSERT INTO packets (id, stream, packet_seq, sample_idx, payload, received_at)
SELECT id, stream_id, relative_sequence, relative_opus_timestamp, packet, datetime(receive_time/1000000000, 'unixepoch')
FROM discord_voice_packet;

-- Drop old tables
DROP TABLE IF EXISTS discord_voice_stream;
DROP TABLE IF EXISTS discord_voice_packet;

-- Note: We're keeping the transcripts table as it is, since it's still relevant in the new schema
