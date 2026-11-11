-- The baselines of spec §11, ranking the same held-out books from the same
-- pool as the content view, under both protocols (see eval.go):
--
--   same author  books by authors I've finished, the more of theirs the higher
--   same shelf   books on the shelf I've finished the most from, a shelf
--                being a BISAC heading cut to two levels ("Fiction / Thrillers")
--   random       any order at all
--
-- In the history protocol, scores are sums of seed weights, and every score
-- leaves the target's own weight out, as if I hadn't read it. Many books
-- tie, and ties are broken at random, so rather than a rank this returns,
-- for each target, how many candidates score higher than it and how many
-- tie with it (itself included). Go turns that into the expected rank.
WITH finished AS (
    SELECT work_id, CASE WHEN $unweighted THEN 1.0 ELSE seed_weight END AS weight
    FROM work_signal
    WHERE finished
),
-- What could be recommended: borrowable works with a vector, as in the
-- content view. Targets are the finished ones; candidates the rest.
pool AS (
    SELECT b.work_id
    FROM borrowable b
    JOIN work_text t USING (work_id)
    JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
),
target AS (
    SELECT f.work_id, f.weight FROM finished f JOIN pool USING (work_id)
),
candidate AS (
    SELECT work_id FROM pool WHERE work_id NOT IN (SELECT work_id FROM finished)
),
n AS (
    SELECT count(*) AS candidates FROM candidate
),

-- Same author. A work's author is the key matching uses ("carre j"), so
-- that "John le Carré" and "John Le Carre" are one person.
author AS (
    SELECT wr.work_id, mode(k.author_key) AS author_key
    FROM work_record wr
    JOIN record_key k USING (ref)
    WHERE k.author_key <> ''
    GROUP BY wr.work_id
),
author_weight AS (
    SELECT a.author_key, sum(f.weight) AS weight
    FROM finished f
    JOIN author a USING (work_id)
    GROUP BY a.author_key
),
author_score AS (  -- before holding anything out
    SELECT p.work_id, a.author_key, coalesce(aw.weight, 0) AS score
    FROM pool p
    LEFT JOIN author a USING (work_id)
    LEFT JOIN author_weight aw USING (author_key)
),
by_author AS (
    SELECT target_id, 'same author' AS method,
        count(*) FILTER (WHERE c_score > t_score) AS higher,
        count(*) FILTER (WHERE c_score = t_score) + 1 AS ties
    FROM (
        SELECT
            t.work_id AS target_id,
            ts.score - CASE WHEN ts.author_key IS NULL THEN 0 ELSE t.weight END AS t_score,
            cs.score - CASE WHEN cs.author_key = ts.author_key THEN t.weight ELSE 0 END AS c_score
        FROM target t
        JOIN author_score ts USING (work_id)
        CROSS JOIN (SELECT * FROM author_score WHERE work_id IN (SELECT work_id FROM candidate)) cs
    )
    GROUP BY target_id
),

-- Same shelf. A work's score is its best shelf's weight; works on no
-- shelf I've read from score 0 and are left out of the pairs below.
shelf AS (
    SELECT DISTINCT
        wr.work_id,
        lower(array_to_string(string_split(s.name, ' / ')[1:2], ' / ')) AS shelf
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id,
         unnest(ct.subjects) AS u(s)
    WHERE s.scheme = 'bisac'
),
shelf_weight AS (
    SELECT sh.shelf, sum(f.weight) AS weight
    FROM finished f
    JOIN shelf sh USING (work_id)
    GROUP BY sh.shelf
),
pool_shelf AS (
    SELECT sh.work_id, sh.shelf, sw.weight, sh.work_id IN (SELECT work_id FROM candidate) AS is_candidate
    FROM shelf sh
    JOIN shelf_weight sw USING (shelf)
    WHERE sh.work_id IN (SELECT work_id FROM pool)
),
target_shelf AS (
    SELECT t.work_id, sh.shelf FROM target t JOIN shelf sh USING (work_id)
),
-- Every target paired with itself and each candidate on a shelf I've read
-- from, scored with the target held out of its own shelves.
shelf_pair AS (
    SELECT
        t.work_id AS target_id,
        ps.work_id,
        max(ps.weight - CASE WHEN ts.shelf IS NULL THEN 0 ELSE t.weight END) AS score
    FROM target t
    CROSS JOIN pool_shelf ps
    LEFT JOIN target_shelf ts ON ts.work_id = t.work_id AND ts.shelf = ps.shelf
    WHERE ps.is_candidate OR ps.work_id = t.work_id
    GROUP BY t.work_id, ps.work_id
),
shelf_target AS (
    SELECT t.work_id AS target_id, coalesce(sp.score, 0) AS t_score
    FROM target t
    LEFT JOIN shelf_pair sp ON sp.target_id = t.work_id AND sp.work_id = t.work_id
),
by_shelf AS (
    SELECT st.target_id, 'same shelf' AS method,
        count(sp.work_id) FILTER (WHERE sp.score > st.t_score) AS higher,
        count(sp.work_id) FILTER (WHERE sp.score = st.t_score) + 1
            -- the candidates on no shelf of mine tie with a target scoring 0
            + CASE WHEN st.t_score = 0 THEN n.candidates - count(sp.work_id) ELSE 0 END AS ties
    FROM shelf_target st
    CROSS JOIN n
    LEFT JOIN shelf_pair sp ON sp.target_id = st.target_id AND sp.work_id <> st.target_id
    GROUP BY st.target_id, st.t_score, n.candidates
),

by_chance AS (
    SELECT t.work_id AS target_id, 'random' AS method, 0 AS higher, n.candidates + 1 AS ties
    FROM target t
    CROSS JOIN n
),

-- The one-seed protocol: each other finished book alone as the seed, and
-- the target's best rank from any of them. ties is NULL when no seed can
-- rank the target at all.
--
-- Same author: a seed lists its author's books, in random order. Any other
-- finished book by the target's author does equally well.
candidate_author AS (
    SELECT a.author_key, count(*) AS n
    FROM candidate c
    JOIN author a USING (work_id)
    GROUP BY a.author_key
),
seed_by_author AS (
    SELECT
        t.work_id AS target_id, 'same author' AS method, 0 AS higher,
        CASE WHEN EXISTS (
            SELECT 1 FROM finished f JOIN author fa USING (work_id)
            WHERE fa.author_key = ta.author_key AND f.work_id <> t.work_id
        ) THEN 1 + coalesce(ca.n, 0) END AS ties
    FROM target t
    LEFT JOIN author ta USING (work_id)
    LEFT JOIN candidate_author ca USING (author_key)
),
-- Same shelf: a seed lists the books on one of its shelves, in random
-- order. The best seed shares the target's smallest shelf.
shelf_size AS (
    SELECT sh.shelf, count(*) AS n
    FROM candidate c
    JOIN shelf sh USING (work_id)
    GROUP BY sh.shelf
),
seed_by_shelf AS (
    SELECT
        t.work_id AS target_id, 'same shelf' AS method, 0 AS higher,
        (SELECT min(1 + coalesce(ss.n, 0))
         FROM target_shelf ts
         LEFT JOIN shelf_size ss USING (shelf)
         WHERE ts.work_id = t.work_id
           AND EXISTS (
               SELECT 1 FROM finished f JOIN shelf fs USING (work_id)
               WHERE fs.shelf = ts.shelf AND f.work_id <> t.work_id)
        ) AS ties
    FROM target t
)

SELECT target_id, 'history' AS protocol, method, higher, ties FROM by_author
UNION ALL
SELECT target_id, 'history', method, higher, ties FROM by_shelf
UNION ALL
SELECT target_id, 'history', method, higher, ties FROM by_chance
UNION ALL
SELECT target_id, 'one seed', method, higher, ties FROM seed_by_author
UNION ALL
SELECT target_id, 'one seed', method, higher, ties FROM seed_by_shelf
UNION ALL
SELECT target_id, 'one seed', method, higher, ties FROM by_chance
