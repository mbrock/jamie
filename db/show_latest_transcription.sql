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
    tw.attaches_to,
    wa.content,
    wa.confidence,
    string_agg(
        CASE WHEN wa2.confidence = max_confidence.max_conf THEN wa2.content ELSE NULL END,
        ' '
        ORDER BY tw.start_time
    ) OVER (PARTITION BY ts.id) AS transcript
FROM latest_session ls
JOIN transcription_segments ts ON ts.session_id = ls.id
JOIN transcription_words tw ON tw.segment_id = ts.id AND tw.version = ts.version
JOIN word_alternatives wa ON wa.word_id = tw.id
JOIN LATERAL (
    SELECT MAX(confidence) AS max_conf
    FROM word_alternatives
    WHERE word_id = tw.id
) max_confidence ON TRUE
JOIN word_alternatives wa2 ON wa2.word_id = tw.id
ORDER BY ts.id, tw.start_time, wa.confidence DESC;
