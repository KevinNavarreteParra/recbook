-- The content vectors of some works, for diversifying a list of them.
SELECT t.work_id, e.vector
FROM work_text t
JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
WHERE t.work_id IN (SELECT unnest($ids::BIGINT[]))
