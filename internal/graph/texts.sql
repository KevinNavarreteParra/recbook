-- Every text in work_text that has a vector in the model, once.
SELECT DISTINCT e.text_hash, e.vector
FROM work_text t
JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
ORDER BY e.text_hash
