-- Each work's best description, for the content view (spec §3.5), with the
-- title of the record it came from.
--
-- A description in one of my languages comes first (one whose language a
-- source doesn't record counts as mine): a seed described in Spanish has
-- Spanish neighbours, so if the library has a book only in Spanish but my
-- reading app describes it in English, the English stands for it. Then the
-- catalog's blurb, because every candidate is described by one, and
-- similarity is only fair between texts of the same kind. Otherwise a shelf
-- description stands in; Reading List's come from Google Books and are the
-- same kind of publisher copy. Then the longest, and the ref, so the choice,
-- and with it the text to embed, is the same on every build.
WITH described AS (
    SELECT wr.work_id, wr.ref, ct.title, ct.description, 'catalog' AS side, 'catalog' AS source, ct.language
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    WHERE ct.description IS NOT NULL
    UNION ALL
    SELECT wr.work_id, wr.ref, si.title, si.description, 'shelf', si.source, si.language
    FROM work_record wr
    JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id
    WHERE si.description IS NOT NULL
)
SELECT d.work_id, d.title, d.description, d.source, d.language, w.on_shelf
FROM described d
JOIN work w USING (work_id)
QUALIFY row_number() OVER (
    PARTITION BY d.work_id
    ORDER BY
        (len($languages::VARCHAR[]) = 0 OR d.language IS NULL OR list_contains($languages::VARCHAR[], d.language)) DESC,
        d.side = 'catalog' DESC,
        length(d.description) DESC,
        d.ref) = 1
-- Shelf works first: they are the seeds, so they matter most if a night ends early.
ORDER BY w.on_shelf DESC, d.work_id
