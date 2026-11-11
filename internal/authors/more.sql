-- More by this author (spec §8): their books the library can lend that I
-- haven't finished, most borrowable first, as the backlog sorts.
SELECT
    b.work_id, b.title_id, b.title, coalesce(b.author, ''), coalesce(b.narrator, ''), b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, coalesce(b.url, ''), coalesce(s.tier, '')
FROM author_work aw
JOIN borrowable b USING (work_id)
LEFT JOIN work_signal s USING (work_id)
WHERE aw.author_id = $author
  AND ($include_read OR NOT coalesce(s.finished, false))
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
ORDER BY b.available DESC, b.wait_days NULLS LAST, b.title
LIMIT $limit
