-- Update streams table
UPDATE streams SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE streams RENAME COLUMN created_at TO created_at_old;
ALTER TABLE streams ADD COLUMN created_at REAL;
UPDATE streams SET created_at = created_at_old;

-- Update packets table
UPDATE packets SET received_at = julianday(received_at) WHERE received_at IS NOT NULL;
ALTER TABLE packets RENAME COLUMN received_at TO received_at_old;
ALTER TABLE packets ADD COLUMN received_at REAL;
UPDATE packets SET received_at = received_at_old;

-- Update speakers table
UPDATE speakers SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE speakers RENAME COLUMN created_at TO created_at_old;
ALTER TABLE speakers ADD COLUMN created_at REAL;
UPDATE speakers SET created_at = created_at_old;

-- Update discord_speakers table
UPDATE discord_speakers SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE discord_speakers RENAME COLUMN created_at TO created_at_old;
ALTER TABLE discord_speakers ADD COLUMN created_at REAL;
UPDATE discord_speakers SET created_at = created_at_old;

-- Update discord_channel_streams table
UPDATE discord_channel_streams SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE discord_channel_streams RENAME COLUMN created_at TO created_at_old;
ALTER TABLE discord_channel_streams ADD COLUMN created_at REAL;
UPDATE discord_channel_streams SET created_at = created_at_old;

-- Update attributions table
UPDATE attributions SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE attributions RENAME COLUMN created_at TO created_at_old;
ALTER TABLE attributions ADD COLUMN created_at REAL;
UPDATE attributions SET created_at = created_at_old;

-- Update recognitions table
UPDATE recognitions SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE recognitions RENAME COLUMN created_at TO created_at_old;
ALTER TABLE recognitions ADD COLUMN created_at REAL;
UPDATE recognitions SET created_at = created_at_old;

-- Update speech_recognition_sessions table
UPDATE speech_recognition_sessions SET created_at = julianday(created_at) WHERE created_at IS NOT NULL;
ALTER TABLE speech_recognition_sessions RENAME COLUMN created_at TO created_at_old;
ALTER TABLE speech_recognition_sessions ADD COLUMN created_at REAL;
UPDATE speech_recognition_sessions SET created_at = created_at_old;

-- Drop old columns after all updates are complete
DROP TABLE IF EXISTS old_columns_to_drop;
CREATE TEMPORARY TABLE old_columns_to_drop (
    table_name TEXT,
    column_name TEXT
);

INSERT INTO old_columns_to_drop (table_name, column_name) VALUES
    ('streams', 'created_at_old'),
    ('packets', 'received_at_old'),
    ('speakers', 'created_at_old'),
    ('discord_speakers', 'created_at_old'),
    ('discord_channel_streams', 'created_at_old'),
    ('attributions', 'created_at_old'),
    ('recognitions', 'created_at_old'),
    ('speech_recognition_sessions', 'created_at_old');

-- Loop through the temporary table and drop columns if they exist
CREATE TEMPORARY TABLE pragma_table_info_results AS
SELECT * FROM pragma_table_info('streams') WHERE 0;

INSERT INTO pragma_table_info_results
SELECT * FROM pragma_table_info('streams')
UNION ALL SELECT * FROM pragma_table_info('packets')
UNION ALL SELECT * FROM pragma_table_info('speakers')
UNION ALL SELECT * FROM pragma_table_info('discord_speakers')
UNION ALL SELECT * FROM pragma_table_info('discord_channel_streams')
UNION ALL SELECT * FROM pragma_table_info('attributions')
UNION ALL SELECT * FROM pragma_table_info('recognitions')
UNION ALL SELECT * FROM pragma_table_info('speech_recognition_sessions');

DELETE FROM old_columns_to_drop
WHERE (table_name, column_name) NOT IN (
    SELECT tbl_name, name
    FROM pragma_table_info_results
);

CREATE TEMPORARY TABLE drop_column_statements (stmt TEXT);

INSERT INTO drop_column_statements (stmt)
SELECT 'ALTER TABLE ' || table_name || ' DROP COLUMN ' || column_name || ';'
FROM old_columns_to_drop;

CREATE TEMPORARY TABLE execution_results (result TEXT);

WITH RECURSIVE
    execution(stmt, rest) AS (
        SELECT NULL, (SELECT group_concat(stmt, CHAR(10)) FROM drop_column_statements)
        UNION ALL
        SELECT
            substr(rest, 0, instr(rest, CHAR(10))),
            substr(rest, instr(rest, CHAR(10)) + 1)
        FROM execution
        WHERE rest <> ''
    )
INSERT INTO execution_results
SELECT CASE WHEN stmt IS NULL THEN 'No statements to execute'
            WHEN stmt = '' THEN 'Empty statement'
            ELSE 'Executed: ' || stmt
       END
FROM execution WHERE stmt IS NOT NULL;

SELECT * FROM execution_results;

DROP TABLE IF EXISTS old_columns_to_drop;
DROP TABLE IF EXISTS pragma_table_info_results;
DROP TABLE IF EXISTS drop_column_statements;
DROP TABLE IF EXISTS execution_results;
