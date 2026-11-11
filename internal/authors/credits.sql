-- author_credit: who wrote each work, as the sources credit them. A work's
-- catalog credits win when it has any: they come with roles, so a narrator
-- or an editor is never taken for an author, and their spelling is the
-- tidiest. A work the library doesn't have keeps its shelf credits.
CREATE OR REPLACE TABLE author_credit AS
WITH catalog_credit AS (
    SELECT DISTINCT wr.work_id, c.name
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id,
         unnest(list_filter(ct.creators, lambda c: c.role = 'author')) AS u(c)
),
shelf_credit AS (
    SELECT DISTINCT wr.work_id, u.name
    FROM work_record wr
    JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id,
         unnest(si.authors) AS u(name)
)
SELECT work_id, name, true AS from_catalog FROM catalog_credit
UNION ALL
SELECT work_id, name, false FROM shelf_credit
WHERE work_id NOT IN (SELECT work_id FROM catalog_credit);

-- author_name: the person each credited name stands for, filled in by Go
-- (textkey.Person). Placeholders like "Anonymous" get no row.
CREATE OR REPLACE TABLE author_name (
    name      VARCHAR PRIMARY KEY,
    author_id VARCHAR NOT NULL
);
