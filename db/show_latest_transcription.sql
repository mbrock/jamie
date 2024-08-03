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
    string_agg(
        CASE 
            WHEN tw.is_eos THEN wa.content || '.'
            ELSE wa.content
        END,
        ' '
        ORDER BY tw.start_time
    ) AS transcript,
    json_agg(
        json_build_object(
            'word_id', tw.id,
            'content', wa.content,
            'confidence', ROUND(wa.confidence::numeric, 2),
            'start_time', tw.start_time,
            'duration', tw.duration,
            'is_eos', tw.is_eos
        )
        ORDER BY tw.start_time
    ) AS words_details
FROM latest_session ls
JOIN transcription_segments ts ON ts.session_id = ls.id
JOIN transcription_words tw ON tw.segment_id = ts.id AND tw.version = ts.version
JOIN LATERAL (
    SELECT content, confidence
    FROM word_alternatives
    WHERE word_id = tw.id
    ORDER BY confidence DESC
    LIMIT 1
) wa ON true
GROUP BY ts.id, ts.is_final, ts.version
ORDER BY ts.id;
