-- Update streams table
ALTER TABLE streams ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE streams SET created_at_new = created_at;
ALTER TABLE streams DROP COLUMN created_at;
ALTER TABLE streams RENAME COLUMN created_at_new TO created_at;

-- Update packets table
ALTER TABLE packets ADD COLUMN received_at_new REAL DEFAULT (julianday('now'));
UPDATE packets SET received_at_new = received_at;
ALTER TABLE packets DROP COLUMN received_at;
ALTER TABLE packets RENAME COLUMN received_at_new TO received_at;

-- Update speakers table
ALTER TABLE speakers ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE speakers SET created_at_new = created_at;
ALTER TABLE speakers DROP COLUMN created_at;
ALTER TABLE speakers RENAME COLUMN created_at_new TO created_at;

-- Update discord_speakers table
ALTER TABLE discord_speakers ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE discord_speakers SET created_at_new = created_at;
ALTER TABLE discord_speakers DROP COLUMN created_at;
ALTER TABLE discord_speakers RENAME COLUMN created_at_new TO created_at;

-- Update discord_channel_streams table
ALTER TABLE discord_channel_streams ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE discord_channel_streams SET created_at_new = created_at;
ALTER TABLE discord_channel_streams DROP COLUMN created_at;
ALTER TABLE discord_channel_streams RENAME COLUMN created_at_new TO created_at;

-- Update attributions table
ALTER TABLE attributions ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE attributions SET created_at_new = created_at;
ALTER TABLE attributions DROP COLUMN created_at;
ALTER TABLE attributions RENAME COLUMN created_at_new TO created_at;

-- Update recognitions table
ALTER TABLE recognitions ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE recognitions SET created_at_new = created_at;
ALTER TABLE recognitions DROP COLUMN created_at;
ALTER TABLE recognitions RENAME COLUMN created_at_new TO created_at;

-- Update speech_recognition_sessions table
ALTER TABLE speech_recognition_sessions ADD COLUMN created_at_new REAL DEFAULT (julianday('now'));
UPDATE speech_recognition_sessions SET created_at_new = created_at;
ALTER TABLE speech_recognition_sessions DROP COLUMN created_at;
ALTER TABLE speech_recognition_sessions RENAME COLUMN created_at_new TO created_at;
