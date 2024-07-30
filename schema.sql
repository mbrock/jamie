-- Discord voice sessions
CREATE TABLE IF NOT EXISTS voice_sessions (
    id TEXT PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    started_at REAL NOT NULL DEFAULT (julianday('now')),
    ended_at REAL
);

-- Voice state events log
CREATE TABLE IF NOT EXISTS voice_state_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    ssrc INTEGER NOT NULL,
    is_speaking BOOLEAN NOT NULL,
    event_time REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (session_id) REFERENCES voice_sessions(id)
);

-- Audio packets
CREATE TABLE IF NOT EXISTS voice_packets (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    ssrc INTEGER NOT NULL,
    packet_seq INTEGER NOT NULL,
    sample_idx INTEGER NOT NULL,
    payload BLOB NOT NULL,
    received_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (session_id) REFERENCES voice_sessions(id)
);

-- Recognition sessions
CREATE TABLE IF NOT EXISTS recognition_sessions (
    id TEXT PRIMARY KEY,
    voice_session_id TEXT NOT NULL,
    ssrc INTEGER NOT NULL,
    start_sample_idx INTEGER NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (voice_session_id) REFERENCES voice_sessions(id)
);

-- Speech recognition results
CREATE TABLE IF NOT EXISTS recognitions (
    id TEXT PRIMARY KEY,
    recognition_session_id TEXT NOT NULL,
    time_offset REAL NOT NULL,
    text TEXT NOT NULL,
    confidence REAL NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    FOREIGN KEY (recognition_session_id) REFERENCES recognition_sessions(id)
);

-- Text messages
CREATE TABLE IF NOT EXISTS text_messages (
    id TEXT PRIMARY KEY,
    discord_channel TEXT NOT NULL,
    discord_user TEXT NOT NULL,
    discord_message_id TEXT NOT NULL,
    content TEXT NOT NULL,
    is_bot BOOLEAN NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now'))
);

-- System prompts
CREATE TABLE IF NOT EXISTS system_prompts (
    name TEXT PRIMARY KEY,
    prompt TEXT NOT NULL,
    created_at REAL NOT NULL DEFAULT (julianday('now')),
    updated_at REAL NOT NULL DEFAULT (julianday('now'))
);

-- View for current voice state
CREATE VIEW IF NOT EXISTS current_voice_state AS
SELECT 
    vse1.session_id,
    vse1.user_id,
    vse1.username,
    vse1.ssrc,
    vse1.is_speaking,
    vse1.event_time
FROM 
    voice_state_events vse1
INNER JOIN (
    SELECT 
        session_id,
        user_id,
        MAX(event_time) as max_event_time
    FROM 
        voice_state_events
    GROUP BY 
        session_id, user_id
) vse2 ON vse1.session_id = vse2.session_id 
    AND vse1.user_id = vse2.user_id 
    AND vse1.event_time = vse2.max_event_time;
