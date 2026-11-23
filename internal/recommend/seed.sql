-- A seed work: its name, how much it says about my taste (its tier's seed
-- weight; 1 for a book I haven't recorded, or that I gave up on but chose
-- as a seed anyway), its vector, if it has been embedded, where the text
-- behind the vector came from and in what language, and whether it has a
-- genre vector.
SELECT
    w.title,
    coalesce(w.author, ''),
    CASE WHEN s.seed_weight > 0 THEN s.seed_weight ELSE 1.0 END,
    e.vector,
    coalesce(t.source, ''),
    coalesce(t.language, ''),
    EXISTS (SELECT 1 FROM genre_vector g WHERE g.work_id = w.work_id)
FROM work w
LEFT JOIN work_signal s USING (work_id)
LEFT JOIN work_text t USING (work_id)
LEFT JOIN work_vector e ON e.model = $model AND e.work_id = w.work_id
WHERE w.work_id = $work_id
