-- The graph's work-to-work edges: each work and the works whose texts are
-- nearest its own (text_neighbour), with their cosine.
SELECT a.work_id, b.work_id, n.similarity
FROM text_neighbour n
JOIN work_text a ON a.text_hash = n.text_hash
JOIN work_text b ON b.text_hash = n.neighbour
WHERE n.model = $model AND a.work_id <> b.work_id
