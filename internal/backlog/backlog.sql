-- The backlog (spec §8): books I own or want but haven't finished that
-- the library has as an audiobook. When a work has several recordings,
-- only one is shown: the full text over an abridged or dramatized version
-- (even if it means a wait), then available now, then the shortest wait.
WITH edition AS (
    SELECT
        s.work_id, s.tier, w.title, w.author,
        ct.library, ct.title_id, ct.duration_min, ct.url,
        list_filter(ct.creators, lambda c: c.role = 'narrator')[1].name AS narrator,
        coalesce(a.available, false) AS available,
        a.wait_days, a.holds,
        -- not the whole book as written: "Abridged", "Dramatized", "Adapted"
        coalesce(ct.edition ILIKE '%abridged%' AND ct.edition NOT ILIKE '%unabridged%'
                 OR ct.edition ILIKE '%dramatiz%' OR ct.edition ILIKE '%adapted%', false) AS abridged
    FROM work_signal s
    JOIN work w USING (work_id)
    JOIN work_record wr USING (work_id)
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    LEFT JOIN latest_availability a ON a.library = ct.library AND a.title_id = ct.title_id
    WHERE s.tier IN ('T4', 'T5')
      AND ct.format = 'audiobook'
),
-- Pick each work's recording before filtering, so --available-now can't
-- swap in an abridged version just because it's on the shelf.
best AS (
    SELECT * FROM edition
    QUALIFY row_number() OVER (
        PARTITION BY work_id
        ORDER BY abridged, available DESC, wait_days NULLS LAST, duration_min DESC NULLS LAST, title_id) = 1
)
SELECT tier, title, author, narrator, available, wait_days, holds, duration_min, abridged, url
FROM best
WHERE (available OR NOT $available_now)
  AND (available OR $max_wait < 0 OR wait_days <= $max_wait)
ORDER BY available DESC, wait_days NULLS LAST, tier, title
LIMIT $limit
