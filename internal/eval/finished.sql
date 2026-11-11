-- The books I've finished. Every one is a seed; those the library lends,
-- and that have a vector to compare, are also held out in turn as targets.
SELECT
    s.work_id,
    w.title,
    coalesce(w.author, ''),
    s.tier,
    b.work_id IS NOT NULL AND e.text_hash IS NOT NULL AS target
FROM work_signal s
JOIN work w USING (work_id)
LEFT JOIN borrowable b USING (work_id)
LEFT JOIN work_text t USING (work_id)
LEFT JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
WHERE s.finished
ORDER BY s.work_id
