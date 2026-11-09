-- work: one row per book, regardless of edition or source. Built from
-- work_record, which Go fills from the match_edge clusters.
CREATE OR REPLACE TABLE work AS
SELECT
    w.work_id,
    -- Show the catalog's title and author when there is one (its metadata
    -- is the tidiest), otherwise the first shelf record's.
    first(r.title  ORDER BY r.side = 'catalog' DESC, r.ref) AS title,
    first(r.author ORDER BY r.side = 'catalog' DESC, r.ref) AS author,
    bool_or(r.side = 'catalog') AS in_catalog,
    bool_or(r.side = 'shelf')   AS on_shelf,
    count(*)                    AS records
FROM work_record w
JOIN record r USING (ref)
GROUP BY w.work_id;

-- review: matches worth a human look, written to the review file.
--   weak matches  the main_title and fuzzy edges themselves
--   mixed works   works whose records disagree on the main title, which is
--                 how a wrong edge chaining two books together shows up
CREATE OR REPLACE TABLE match_review AS
SELECT e.method AS reason, round(e.score, 3) AS score,
       e.a, ra.title || coalesce(': ' || ra.subtitle, '') AS a_title, ra.author AS a_author,
       e.b, rb.title || coalesce(': ' || rb.subtitle, '') AS b_title, rb.author AS b_author
FROM match_edge e
JOIN record ra ON ra.ref = e.a
JOIN record rb ON rb.ref = e.b
WHERE e.method IN ('main_title', 'fuzzy')
UNION ALL
SELECT 'mixed_work', count(DISTINCT k.main_key),
       min(w.ref), string_agg(DISTINCT r.title, ' | '), NULL,
       NULL, NULL, NULL
FROM work_record w
JOIN record_key k USING (ref)
JOIN record r USING (ref)
WHERE k.main_key <> ''
GROUP BY w.work_id
HAVING count(DISTINCT k.main_key) > 1;
