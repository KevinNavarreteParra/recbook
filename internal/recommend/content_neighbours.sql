-- The borrowable works most like one seed in the content view. Vectors are
-- unit length, so their inner product is the cosine similarity.
SELECT
    b.work_id, b.title_id, b.title, b.author, b.narrator, b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, b.url, s.tier,
    list_inner_product(v.vector, $seed::FLOAT[]) AS similarity
FROM borrowable b
JOIN work_vector v ON v.model = $model AND v.work_id = b.work_id
LEFT JOIN work_signal s ON s.work_id = b.work_id
WHERE NOT list_contains($exclude::BIGINT[], b.work_id)
  AND ($include_read OR NOT coalesce(s.finished, false))
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
  AND (len($languages::VARCHAR[]) = 0 OR list_contains($languages::VARCHAR[], b.language))
ORDER BY similarity DESC
LIMIT $k
