-- The graph's edges from works to the other kinds of node (spec §7), one
-- row each: the work, the kind of node, the node's key, and a weight that
-- ranks a work's edges of one kind against each other.
--
--   author    the people author_work credits (internal/authors)
--   series    the catalog's series name, lowercased
--   subject   each feature of the genre view, weighted as there (IDF, so a
--             rare shelf counts for more than a common one)
--   narrator  the catalog's narrators, lowercased
SELECT 'author' AS kind, work_id, author_id AS key, 1.0 AS weight
FROM author_work
UNION ALL
SELECT DISTINCT 'series', wr.work_id, lower(trim(ct.series)), 1.0
FROM work_record wr
JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
WHERE trim(coalesce(ct.series, '')) <> ''
UNION ALL
SELECT 'subject', work_id, feature, weight
FROM genre_vector
UNION ALL
SELECT DISTINCT 'narrator', wr.work_id, lower(trim(c.name)), 1.0
FROM work_record wr
JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id,
     unnest(ct.creators) AS u(c)
WHERE c.role = 'narrator'
