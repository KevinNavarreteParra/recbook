-- The backlog (spec §8): books I own or want but haven't finished that
-- the library has as an audiobook. When a work has several recordings,
-- only the most borrowable one is shown: available now first, then the
-- shortest wait.
WITH edition AS (
    SELECT
        s.work_id, s.tier, w.title, w.author,
        ct.library, ct.title_id, ct.duration_min, ct.url,
        list_filter(ct.creators, lambda c: c.role = 'narrator')[1].name AS narrator,
        coalesce(a.available, false) AS available,
        a.wait_days, a.holds
    FROM work_signal s
    JOIN work w USING (work_id)
    JOIN work_record wr USING (work_id)
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    LEFT JOIN latest_availability a ON a.library = ct.library AND a.title_id = ct.title_id
    WHERE s.tier IN ('T4', 'T5')
      AND ct.format = 'audiobook'
)
SELECT tier, title, author, narrator, available, wait_days, holds, duration_min, url
FROM edition
WHERE (available OR NOT $available_now)
  AND (available OR $max_wait < 0 OR wait_days <= $max_wait)
QUALIFY row_number() OVER (PARTITION BY work_id ORDER BY available DESC, wait_days NULLS LAST, title_id) = 1
ORDER BY available DESC, wait_days NULLS LAST, tier, title
LIMIT $limit
