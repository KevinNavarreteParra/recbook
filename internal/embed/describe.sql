-- Each work's best description, for the content view (spec §3.5). The
-- catalog's blurb comes first when the library has the book, because every
-- candidate is described by one, and similarity is only fair between texts
-- of the same kind. Otherwise a shelf description stands in; Reading List's
-- come from Google Books and are the same kind of publisher copy.
WITH described AS (
    SELECT wr.work_id, ct.description, 'catalog' AS side
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    WHERE ct.description IS NOT NULL
    UNION ALL
    SELECT wr.work_id, si.description, 'shelf'
    FROM work_record wr
    JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id
    WHERE si.description IS NOT NULL
)
SELECT
    w.work_id,
    w.title,
    first(d.description ORDER BY d.side = 'catalog' DESC, length(d.description) DESC) AS description,
    first(d.side ORDER BY d.side = 'catalog' DESC, length(d.description) DESC) AS side,
    w.on_shelf
FROM work w
JOIN described d USING (work_id)
GROUP BY w.work_id, w.title, w.on_shelf
-- Shelf works first: they are the seeds, so they matter most if a night ends early.
ORDER BY w.on_shelf DESC, w.work_id
