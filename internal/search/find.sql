-- Find works by title and author, for naming seeds. Every word must match
-- (so "dune messiah" doesn't find "Dune"), and works on my shelf come
-- first, since a seed is usually a book I've read. Then come works the
-- library offers in one of my languages, or doesn't have at all, before
-- those it offers only in another: "dune" means the Dune I'd listen to.
SELECT w.work_id, w.title, coalesce(w.author, ''), w.on_shelf
FROM (
    SELECT
        doc_id,
        fts_main_search_doc.match_bm25(doc_id, $q, fields := 'title,authors', conjunctive := 1) AS score
    FROM search_doc
) d
JOIN work w ON w.work_id = d.doc_id
LEFT JOIN borrowable b ON b.work_id = w.work_id
WHERE d.score IS NOT NULL
ORDER BY
    w.on_shelf DESC,
    (b.work_id IS NULL OR len($languages::VARCHAR[]) = 0 OR list_contains($languages::VARCHAR[], b.language)) DESC,
    d.score DESC
LIMIT 5
