-- The books I borrowed and either finished or gave up on, each scored by
-- how much it resembles the other books I finished (spec §11.2). A borrow
-- that ended unfinished is a "gave up", though for Libby, which records
-- borrows and nothing else, that only means the loan ran out (signals.sql).
--
-- Each score leaves the borrow itself out of the finished books, as if I
-- hadn't read it, so a finished borrow doesn't resemble itself:
--
--   content      mean of the 5 highest cosines with finished books' vectors
--   genre        the same with genre vectors (genre.sql); a book shares no
--                feature with most, which count as 0
--   same author  seed weight of finished books by its author
--   same shelf   seed weight of finished books on its best shelf, a shelf
--                being a BISAC heading cut to two levels, as in baselines.sql
--   shorter      minus its length: not a likeness at all, but a check on
--                what "gave up" means when a loan can just run out
--
-- A score is NULL when the borrow can't be compared in that view at all.
WITH finished AS (
    SELECT work_id, (CASE WHEN $unweighted THEN 1.0 ELSE seed_weight END)::DOUBLE AS weight
    FROM work_signal
    WHERE finished
),
borrow AS (
    SELECT work_id, label, label_confidence
    FROM work_signal
    WHERE borrows > 0 AND label IS NOT NULL
),
vector AS (
    SELECT t.work_id, e.vector
    FROM work_text t
    JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
),

content AS (
    SELECT b.work_id, list_avg(max(list_inner_product(bv.vector, fv.vector), 5)) AS score
    FROM borrow b
    JOIN vector bv USING (work_id)
    JOIN finished f ON f.work_id <> b.work_id
    JOIN vector fv ON fv.work_id = f.work_id
    GROUP BY b.work_id
),

genre_pair AS (
    SELECT b.work_id, g.work_id AS finished_id, sum(bg.weight * g.weight) AS similarity
    FROM borrow b
    JOIN genre_vector bg USING (work_id)
    JOIN genre_vector g USING (feature)
    JOIN finished f ON f.work_id = g.work_id
    WHERE g.work_id <> b.work_id
    GROUP BY b.work_id, g.work_id
),
genre AS (
    SELECT b.work_id,
        CASE WHEN b.work_id IN (SELECT work_id FROM genre_vector)
             THEN coalesce(list_sum(max(p.similarity, 5)), 0) / 5 END AS score
    FROM borrow b
    LEFT JOIN genre_pair p USING (work_id)
    GROUP BY b.work_id
),

author AS (  -- as in baselines.sql
    SELECT wr.work_id, mode(k.author_key) AS author_key
    FROM work_record wr
    JOIN record_key k USING (ref)
    WHERE k.author_key <> ''
    GROUP BY wr.work_id
),
by_author AS (
    SELECT b.work_id,
        CASE WHEN ba.author_key IS NOT NULL THEN coalesce(sum(f.weight), 0) END AS score
    FROM borrow b
    LEFT JOIN author ba USING (work_id)
    LEFT JOIN author fa ON fa.author_key = ba.author_key AND fa.work_id <> b.work_id
    LEFT JOIN finished f ON f.work_id = fa.work_id
    GROUP BY b.work_id, ba.author_key
),

shelf AS (  -- as in baselines.sql
    SELECT DISTINCT
        wr.work_id,
        lower(array_to_string(string_split(s.name, ' / ')[1:2], ' / ')) AS shelf
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id,
         unnest(ct.subjects) AS u(s)
    WHERE s.scheme = 'bisac'
),
shelf_weight AS (
    SELECT b.work_id, bs.shelf, coalesce(sum(f.weight), 0) AS weight
    FROM borrow b
    JOIN shelf bs USING (work_id)
    LEFT JOIN shelf fs ON fs.shelf = bs.shelf AND fs.work_id <> b.work_id
    LEFT JOIN finished f ON f.work_id = fs.work_id
    GROUP BY b.work_id, bs.shelf
),
by_shelf AS (
    SELECT work_id, max(weight) AS score FROM shelf_weight GROUP BY work_id
)

SELECT b.work_id, b.label = 1 AS finished, b.label_confidence, c.score, g.score, a.score, s.score,
    -l.duration_min::DOUBLE
FROM borrow b
LEFT JOIN content c USING (work_id)
LEFT JOIN genre g USING (work_id)
LEFT JOIN by_author a USING (work_id)
LEFT JOIN by_shelf s USING (work_id)
LEFT JOIN borrowable l USING (work_id)
ORDER BY b.work_id
