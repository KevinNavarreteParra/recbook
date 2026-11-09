-- search_doc: one document per work, merging what every record of it
-- says: the catalog's titles, people, series, subjects and blurbs, and the
-- shelf's titles, authors and subjects. Every work is indexed, not only
-- the borrowable ones, so seeds for recommendations can be looked up too.
CREATE OR REPLACE TABLE search_doc AS
WITH part AS (
    SELECT
        wr.work_id,
        ct.title || coalesce(' ' || ct.subtitle, '') AS title,
        array_to_string(list_transform(list_filter(ct.creators, lambda c: c.role <> 'narrator'), lambda c: c.name), ' ') AS authors,
        array_to_string(list_transform(list_filter(ct.creators, lambda c: c.role = 'narrator'), lambda c: c.name), ' ') AS narrators,
        coalesce(ct.series, '') AS series,
        array_to_string(list_transform(ct.subjects, lambda s: s.name), ' ') AS subjects,
        coalesce(ct.description, '') AS blurb
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    UNION ALL
    SELECT
        wr.work_id,
        si.title || coalesce(' ' || si.subtitle, ''),
        array_to_string(si.authors, ' '),
        '',
        coalesce(si.series, ''),
        array_to_string(si.subjects, ' '),
        coalesce(si.description, '')
    FROM work_record wr
    JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id
)
SELECT
    work_id AS doc_id,
    string_agg(DISTINCT title, ' ')     AS title,
    string_agg(DISTINCT authors, ' ')   AS authors,
    string_agg(DISTINCT narrators, ' ') AS narrators,
    string_agg(DISTINCT series, ' ')    AS series,
    string_agg(DISTINCT subjects, ' ')  AS subjects,
    -- blurbs are HTML; tags would otherwise become words like "p" and "br"
    regexp_replace(string_agg(DISTINCT blurb, ' '), '<[^>]*>', ' ', 'g') AS blurb
FROM part
GROUP BY work_id;

-- A BM25 full-text index over those fields: words are lowercased,
-- accent-folded ("miserables" finds "Misérables") and stemmed ("spies"
-- finds "spy"), and common English words are ignored.
PRAGMA create_fts_index(
    'search_doc', 'doc_id', 'title', 'authors', 'narrators', 'series', 'subjects', 'blurb',
    stemmer = 'english', stopwords = 'english', strip_accents = 1, lower = 1, overwrite = 1);
