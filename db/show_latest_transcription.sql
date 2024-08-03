WITH latest_session AS (
    SELECT id
    FROM transcription_sessions
    ORDER BY start_time DESC
    LIMIT 1
)
SELECT ts.id AS segment_id,
    ts.is_final,
    ts.version,
    string_agg(
        wa.content,
        ' '
        ORDER BY tw.start_time
    ) AS transcript
FROM latest_session ls
    JOIN transcription_segments ts ON ts.session_id = ls.id
    JOIN transcription_words tw ON tw.segment_id = ts.id
    AND tw.version = ts.version
    JOIN LATERAL (
        SELECT content,
            confidence
        FROM word_alternatives
        WHERE word_id = tw.id
        ORDER BY confidence DESC
        LIMIT 1
    ) wa ON TRUE
GROUP BY ts.id,
    ts.is_final,
    ts.version
ORDER BY ts.id;