WITH latest_session AS (
    SELECT id
    FROM transcription_sessions
    ORDER BY start_time DESC
    LIMIT 1
)
SELECT 
    ts.id AS segment_id,
    ts.is_final,
    ts.version,
    tw.id AS word_id,
    tw.start_time,
    tw.duration,
    tw.is_eos,
    string_agg(
        wa.content || ' (' || ROUND(wa.confidence::numeric, 2) || ')',
        ' | '
        ORDER BY wa.confidence DESC
    ) AS alternatives
FROM latest_session ls
JOIN transcription_segments ts ON ts.session_id = ls.id
JOIN transcription_words tw ON tw.segment_id = ts.id
JOIN word_alternatives wa ON wa.word_id = tw.id
GROUP BY ts.id, ts.is_final, ts.version, tw.id, tw.start_time, tw.duration, tw.is_eos
ORDER BY ts.id, tw.start_time;
