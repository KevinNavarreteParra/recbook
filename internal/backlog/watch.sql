-- The recordings to keep an eye on between crawls: every audiobook
-- recording of a backlog book (owned or wanted, not finished), not just
-- the one internal/library offers, so that it can still switch to
-- whichever comes free first.
SELECT DISTINCT ct.title_id
FROM work_signal s
JOIN work_record wr USING (work_id)
JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
WHERE s.tier IN ('T4', 'T5')
  AND ct.library = $library
  AND ct.format = 'audiobook'
ORDER BY ct.title_id
