-- The texts in work_text that the cache has no vector for yet, each once
-- however many works share it. Texts of shelf works come first: they're
-- the seeds, so they matter most if a run is cut short.
SELECT t.text_hash, any_value(t.text) AS text
FROM work_text t
JOIN work w USING (work_id)
WHERE t.text_hash NOT IN (SELECT text_hash FROM embedding WHERE model = $model)
GROUP BY t.text_hash
ORDER BY bool_or(w.on_shelf) DESC, min(t.work_id)
