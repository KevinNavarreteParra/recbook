-- The backlog (spec §8): books I own or want but haven't finished that
-- the library has as an audiobook, in the recording chosen by
-- internal/library (the full text, the most borrowable).
SELECT b.work_id, b.title_id, s.tier, b.title, b.author, b.narrator, b.available, b.wait_days, b.holds,
       b.duration_min, b.abridged, b.url
FROM work_signal s
JOIN borrowable b USING (work_id)
WHERE s.tier IN ('T4', 'T5')
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
ORDER BY b.available DESC, b.wait_days NULLS LAST, s.tier, b.title
LIMIT $limit
