-- Find works by title and author, for naming seeds. Every word must match
-- (so "dune messiah" doesn't find "Dune"), and works on my shelf come
-- first, since a seed is usually a book I've read.
SELECT w.work_id, w.title, coalesce(w.author, ''), w.on_shelf
FROM (
    SELECT
        doc_id,
        fts_main_search_doc.match_bm25(doc_id, $q, fields := 'title,authors', conjunctive := 1) AS score
    FROM search_doc
) d
JOIN work w ON w.work_id = d.doc_id
WHERE d.score IS NOT NULL
ORDER BY w.on_shelf DESC, d.score DESC
LIMIT 5
