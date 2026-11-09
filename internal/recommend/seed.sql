-- A seed work: its name, how much it says about my taste (its tier's seed
-- weight; 1 for a book I haven't recorded, or that I gave up on but chose
-- as a seed anyway), and its vector, if it has been embedded.
SELECT
    w.title,
    coalesce(w.author, ''),
    CASE WHEN s.seed_weight > 0 THEN s.seed_weight ELSE 1.0 END,
    e.vector
FROM work w
LEFT JOIN work_signal s USING (work_id)
LEFT JOIN work_text t USING (work_id)
LEFT JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
WHERE w.work_id = $work_id
