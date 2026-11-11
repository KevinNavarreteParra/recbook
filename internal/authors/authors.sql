-- author_work: each author's works. A co-written work belongs to each of
-- its authors.
CREATE OR REPLACE TABLE author_work AS
SELECT DISTINCT n.author_id, c.work_id
FROM author_credit c
JOIN author_name n USING (name);

-- author: one row per person, under the spelling of their name the
-- catalog uses most. On a tie the spelling with accents wins, since
-- sources drop accents far more often than they add them ("John le Carré",
-- not "John Le Carre").
CREATE OR REPLACE TABLE author AS
WITH spelling AS (
    SELECT n.author_id, c.name, bool_or(c.from_catalog) AS from_catalog, count(*) AS credits
    FROM author_credit c
    JOIN author_name n USING (name)
    GROUP BY n.author_id, c.name
),
display AS (
    SELECT author_id, name
    FROM spelling
    QUALIFY row_number() OVER (
        PARTITION BY author_id
        ORDER BY from_catalog DESC, credits DESC, name <> strip_accents(name) DESC, name) = 1
)
SELECT
    d.author_id,
    d.name,
    count(*)                                   AS works,
    count(*) FILTER (w.in_catalog)             AS in_catalog,
    count(*) FILTER (w.on_shelf)               AS on_shelf,
    count(*) FILTER (coalesce(s.finished, false)) AS finished
FROM display d
JOIN author_work aw USING (author_id)
JOIN work w USING (work_id)
LEFT JOIN work_signal s USING (work_id)
GROUP BY d.author_id, d.name;
