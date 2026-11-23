-- The stored "for you" list, in its order, keeping what the filters allow
-- and leaving out books I've finished since it was made.
SELECT
    b.work_id, b.title_id, b.title, b.author, b.narrator, b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, b.url, s.tier,
    f."like", f.similarity, f.view
FROM for_you f
JOIN borrowable b USING (work_id)
LEFT JOIN work_signal s USING (work_id)
WHERE NOT coalesce(s.finished, false)
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
  AND ($min_minutes = 0 OR b.duration_min >= $min_minutes)
  AND ($max_minutes = 0 OR b.duration_min < $max_minutes)
  AND (len($languages::VARCHAR[]) = 0 OR list_contains($languages::VARCHAR[], b.language))
ORDER BY f.rank
LIMIT $limit
