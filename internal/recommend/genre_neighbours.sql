-- The borrowable works most like one seed in the genre view: the cosine of
-- their genre vectors, summed over the features they share (genre.sql).
-- Works shelved exactly alike tie, and plenty do; ties go in a fixed but
-- arbitrary order, by a hash of the id rather than the id itself, which
-- follows the catalog's numbering.
WITH near AS (
    SELECT g.work_id, sum(s.weight * g.weight) AS similarity
    FROM genre_vector s
    JOIN genre_vector g USING (feature)
    WHERE s.work_id = $seed
    GROUP BY g.work_id
)
SELECT
    b.work_id, b.title_id, b.title, b.author, b.narrator, b.duration_min, b.abridged,
    b.available, b.wait_days, b.holds, b.url, s.tier,
    n.similarity
FROM near n
JOIN borrowable b USING (work_id)
LEFT JOIN work_signal s USING (work_id)
WHERE NOT list_contains($exclude::BIGINT[], b.work_id)
  AND ($include_read OR NOT coalesce(s.finished, false))
  AND (b.available OR NOT $available_now)
  AND (b.available OR $max_wait < 0 OR b.wait_days <= $max_wait)
ORDER BY n.similarity DESC, hash(b.work_id)
LIMIT $k
