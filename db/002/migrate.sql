-- Update streams table
UPDATE streams SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE streams RENAME COLUMN created_at TO created_at_old;
ALTER TABLE streams ADD COLUMN created_at REAL;
UPDATE streams SET created_at = created_at_old;
ALTER TABLE streams DROP COLUMN created_at_old;

-- Update packets table
UPDATE packets SET received_at = julianday(received_at) WHERE received_at IS NOT NULL;
ALTER TABLE packets RENAME COLUMN received_at TO received_at_old;
ALTER TABLE packets ADD COLUMN received_at REAL;
UPDATE packets SET received_at = received_at_old;
ALTER TABLE packets DROP COLUMN received_at_old;

-- Update speakers table
UPDATE speakers SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE speakers RENAME COLUMN created_at TO created_at_old;
ALTER TABLE speakers ADD COLUMN created_at REAL;
UPDATE speakers SET created_at = created_at_old;
ALTER TABLE speakers DROP COLUMN created_at_old;

-- Update discord_speakers table
UPDATE discord_speakers SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE discord_speakers RENAME COLUMN created_at TO created_at_old;
ALTER TABLE discord_speakers ADD COLUMN created_at REAL;
UPDATE discord_speakers SET created_at = created_at_old;
ALTER TABLE discord_speakers DROP COLUMN created_at_old;

-- Update discord_channel_streams table
UPDATE discord_channel_streams SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE discord_channel_streams RENAME COLUMN created_at TO created_at_old;
ALTER TABLE discord_channel_streams ADD COLUMN created_at REAL;
UPDATE discord_channel_streams SET created_at = created_at_old;
ALTER TABLE discord_channel_streams DROP COLUMN created_at_old;

-- Update attributions table
UPDATE attributions SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE attributions RENAME COLUMN created_at TO created_at_old;
ALTER TABLE attributions ADD COLUMN created_at REAL;
UPDATE attributions SET created_at = created_at_old;
ALTER TABLE attributions DROP COLUMN created_at_old;

-- Update recognitions table
UPDATE recognitions SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE recognitions RENAME COLUMN created_at TO created_at_old;
ALTER TABLE recognitions ADD COLUMN created_at REAL;
UPDATE recognitions SET created_at = created_at_old;
ALTER TABLE recognitions DROP COLUMN created_at_old;

-- Update speech_recognition_sessions table
UPDATE speech_recognition_sessions SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE speech_recognition_sessions RENAME COLUMN created_at TO created_at_old;
ALTER TABLE speech_recognition_sessions ADD COLUMN created_at REAL;
UPDATE speech_recognition_sessions SET created_at = created_at_old;
ALTER TABLE speech_recognition_sessions DROP COLUMN created_at_old;
