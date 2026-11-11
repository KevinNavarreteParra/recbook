-- The finished books that no source describes, apart from this package's
-- own shelf: the seeds recommend would otherwise skip. Each comes with the
-- ISBNs its shelf records carry, to look it up by, and the shortest title
-- they give it, which is the one least cluttered with series names and
-- author prefixes ("Hegel: Elements of the Philosophy of Right (Cambridge
-- Texts in ...)" against "Elements of the Philosophy of Right").
WITH described AS (
    SELECT wr.work_id
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    WHERE ct.description IS NOT NULL
    UNION
    SELECT wr.work_id
    FROM work_record wr
    JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id
    WHERE si.description IS NOT NULL AND si.source <> $source
)
SELECT
    w.work_id,
    arg_min(si.title, length(si.title)) AS title,
    coalesce(w.author, ''),
    list_sort(list_distinct(flatten(list(si.isbns)))) AS isbns
FROM work_signal s
JOIN work w USING (work_id)
JOIN work_record wr USING (work_id)
JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id
WHERE s.finished
  AND si.source <> $source
  AND w.work_id NOT IN (SELECT work_id FROM described)
GROUP BY w.work_id, w.author
ORDER BY w.work_id
