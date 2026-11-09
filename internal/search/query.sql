-- Keyword search over the borrowable works. Any word may match; a work
-- scores by BM25 over all its fields, plus extra for matches in the title
-- and the author (usually what a search is for), plus the most for
-- matching every word: "james webb telescope" should find books about the
-- telescope before books by people named Webb.
WITH hit AS (
    SELECT
        doc_id AS work_id,
        fts_main_search_doc.match_bm25(doc_id, $q)                      AS s_all,
        fts_main_search_doc.match_bm25(doc_id, $q, fields := 'title')   AS s_title,
        fts_main_search_doc.match_bm25(doc_id, $q, fields := 'authors') AS s_author,
        fts_main_search_doc.match_bm25(doc_id, $q, conjunctive := 1)    AS s_every
    FROM search_doc
)
SELECT
    b.work_id, b.title_id, b.title, b.subtitle, b.author, b.narrator, b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, b.url, s.tier,
    hit.s_all + 2 * coalesce(hit.s_title, 0) + 2 * coalesce(hit.s_author, 0)
        + 4 * coalesce(hit.s_every, 0) AS score
FROM hit
JOIN borrowable b USING (work_id)
LEFT JOIN work_signal s USING (work_id)
WHERE hit.s_all IS NOT NULL
  AND ($include_read OR NOT coalesce(s.finished, false))
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
  AND ($min_minutes <= 0 OR b.duration_min >= $min_minutes)
  AND ($max_minutes <= 0 OR b.duration_min <= $max_minutes)
ORDER BY score DESC, b.available DESC
LIMIT $limit
