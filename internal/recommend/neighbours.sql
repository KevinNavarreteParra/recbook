-- The borrowable works most like one seed in the content view. Vectors are
-- unit length, so their inner product is the cosine similarity.
SELECT
    b.work_id, b.title_id, b.title, b.author, b.narrator, b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, b.url, s.tier,
    list_inner_product(e.vector, $seed::FLOAT[]) AS similarity
FROM borrowable b
JOIN work_text t USING (work_id)
JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
LEFT JOIN work_signal s USING (work_id)
WHERE NOT list_contains($exclude::BIGINT[], b.work_id)
  AND ($include_read OR NOT coalesce(s.finished, false))
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
ORDER BY similarity DESC
LIMIT $k
