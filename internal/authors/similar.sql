-- Similar authors (spec §8): the authors whose vectors are nearest this
-- author's, each with their borrowable books nearest it. Choosing a
-- neighbour's books by the queried author's vector, rather than by the
-- neighbour's own, picks the bridge: the Alan Furst most like le Carré,
-- not Furst's most typical book.
--
-- A neighbour needs $min_works embedded works: the mean of one book is
-- just that book, and there are so many one-book authors that the closest
-- few are always there, crowding out authors with a body of work. Books
-- credited to the
-- queried author are left out (they're in "more by"), so a co-author shows
-- only for what they wrote without them, and drops out if that's nothing.
--
-- Only the $pool nearest authors are considered, which is what keeps this
-- quick: comparing every author's every book takes seconds. Authors left
-- with no books under the filters drop out, so $pool is a few times
-- $authors.
WITH me AS (
    SELECT vector FROM author_vector WHERE author_id = $author AND model = $model
),
near AS (
    SELECT v.author_id, v.works AS embedded, list_inner_product(v.vector, me.vector) AS author_similarity
    FROM author_vector v, me
    WHERE v.model = $model AND v.author_id <> $author AND v.works >= $min_works
    ORDER BY author_similarity DESC
    LIMIT $pool
),
book AS (
    SELECT
        n.author_id, n.embedded, n.author_similarity,
        b.work_id, b.title_id, b.title, b.author, b.narrator, b.duration_min, b.abridged,
        b.available, b.wait_days, b.holds, b.url, s.tier,
        -- measured, like the authors, from content_mean
        list_cosine_similarity(list_transform(range(1, len(e.vector) + 1), lambda i: e.vector[i] - c.vector[i]),
                               me.vector) AS similarity
    FROM near n
    JOIN author_work aw USING (author_id)
    JOIN borrowable b USING (work_id)
    JOIN work_text t USING (work_id)
    JOIN embedding e ON e.model = $model AND e.text_hash = t.text_hash
    JOIN content_mean c ON c.model = $model
    CROSS JOIN me
    LEFT JOIN work_signal s USING (work_id)
    WHERE b.work_id NOT IN (SELECT work_id FROM author_work WHERE author_id = $author)
      AND ($include_read OR NOT coalesce(s.finished, false))
      AND (b.available OR NOT $available_now)
      AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
    QUALIFY row_number() OVER (PARTITION BY n.author_id ORDER BY similarity DESC, b.work_id) <= $per_author
),
ranked AS (
    SELECT *, dense_rank() OVER (ORDER BY author_similarity DESC, author_id) AS author_rank
    FROM book
)
SELECT
    a.author_id, a.name, a.works, a.in_catalog, a.on_shelf, a.finished,
    r.embedded, r.author_similarity,
    r.work_id, r.title_id, r.title, coalesce(r.author, ''), coalesce(r.narrator, ''), r.duration_min, r.abridged,
    r.available, r.wait_days, r.holds, coalesce(r.url, ''), coalesce(r.tier, ''), r.similarity
FROM ranked r
JOIN author a USING (author_id)
WHERE r.author_rank <= $authors
ORDER BY r.author_rank, r.similarity DESC
