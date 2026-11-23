-- The borrowable works a walk from one seed reaches most in the graph view,
-- given the walk's best works and their scores ($ids, $scores), filtered
-- as the other views are.
WITH walked AS (
    SELECT unnest($ids::BIGINT[]) AS work_id, unnest($scores::DOUBLE[]) AS similarity
)
SELECT
    b.work_id, b.title_id, b.title, b.author, b.narrator, b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, b.url, s.tier,
    w.similarity
FROM walked w
JOIN borrowable b USING (work_id)
LEFT JOIN work_signal s USING (work_id)
WHERE NOT list_contains($exclude::BIGINT[], b.work_id)
  AND ($include_read OR NOT coalesce(s.finished, false))
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
  AND (len($languages::VARCHAR[]) = 0 OR list_contains($languages::VARCHAR[], b.language))
ORDER BY w.similarity DESC, b.work_id
LIMIT $k
