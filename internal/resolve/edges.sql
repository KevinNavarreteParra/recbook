-- match_edge: pairs of records that look like the same work, and why.
-- Edges are undirected and stored once, with a < b. All of them are
-- accepted unless match_overrides.csv says otherwise; the weak kinds
-- (main_title, fuzzy) are also written to the review file.
CREATE OR REPLACE TABLE match_edge AS
WITH r AS (
    SELECT * FROM record JOIN record_key USING (ref)
),
isbn AS (
    SELECT ref, unnest(isbns) AS isbn FROM r
),
xref AS (
    -- unnesting two lists in one SELECT zips them: one row per map entry
    SELECT ref, unnest(map_keys(refs)) AS system, unnest(map_values(refs)) AS id FROM r
),
-- Weak matches only start from shelf records: the catalog is matched to
-- itself on exact evidence alone, so near-identical series titles in a
-- big collection never chain together.
shelf AS (
    SELECT * FROM r WHERE side = 'shelf' AND author_key <> '' AND title_key <> ''
),
edges AS (
    -- The same ISBN: the same edition.
    SELECT a.ref AS a, b.ref AS b, 'isbn' AS method, 1.0 AS score
    FROM isbn a JOIN isbn b ON a.isbn = b.isbn AND a.ref < b.ref

    UNION ALL
    -- The same id in another system: an OverDrive id on a Libby loan, a
    -- LibraryThing workcode on two editions.
    SELECT a.ref, b.ref, 'ref:' || a.system, 1.0
    FROM xref a JOIN xref b ON a.system = b.system AND a.id = b.id AND a.ref < b.ref

    UNION ALL
    -- The same title and author once normalized.
    SELECT a.ref, b.ref, 'title', 1.0
    FROM r a JOIN r b ON a.title_key = b.title_key AND a.author_key = b.author_key AND a.ref < b.ref
    WHERE a.title_key <> '' AND a.author_key <> ''

    UNION ALL
    -- The same main title and author, where one side has no subtitle:
    -- "Ethics" and "Ethics: Demonstrated in Geometrical Order".
    SELECT least(a.ref, b.ref), greatest(a.ref, b.ref), 'main_title', 0.9
    FROM shelf a JOIN r b ON a.main_key = b.main_key AND a.author_key = b.author_key AND a.ref <> b.ref
    WHERE a.title_key <> b.title_key
      AND (a.title_key = a.main_key OR b.title_key = b.main_key)

    UNION ALL
    -- Nearly the same title by the same author, to absorb typos and
    -- spelling variants. Titles whose numbers differ never match, so book 2
    -- of a series isn't mistaken for book 3.
    SELECT least(a.ref, b.ref), greatest(a.ref, b.ref), 'fuzzy', jaro_winkler_similarity(a.title_key, b.title_key)
    FROM shelf a JOIN r b ON a.author_key = b.author_key AND a.ref <> b.ref
    WHERE a.title_key <> b.title_key
      AND regexp_extract_all(a.title_key, '\d+') = regexp_extract_all(b.title_key, '\d+')
      AND jaro_winkler_similarity(a.title_key, b.title_key) >= getvariable('fuzzy_min')
)
SELECT DISTINCT a, b, method, score FROM edges;
