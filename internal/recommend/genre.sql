-- genre_vector: each work's genre view (spec §6), a sparse vector over the
-- library's subjects. Weights are IDF, ln(works / works with the subject),
-- so a rare shelf says more than a common one, and each vector has unit
-- length, so the sum of two works' weights multiplied over the features
-- they share is their cosine similarity.
--
-- An OverDrive subject ("Thriller") is one feature. A BISAC heading is a
-- path ("Fiction / Thrillers / Espionage"), and it counts as itself and as
-- each broader heading above it ("Fiction / Thrillers", "Fiction"), so two
-- spy novels shelved under Espionage and under Suspense still share
-- something. A feature on more than a quarter of the catalog's works
-- (Fiction, Literature, Nonfiction) is dropped: it barely separates books,
-- and the content view already knows a novel from a history.
--
-- Works the catalog doesn't carry have only my shelf's subjects, which are
-- another vocabulary. Most are Google Books categories, which are BISAC
-- headings broken into their parts ("Fiction", "Thrillers", "Espionage"),
-- so such a work gets each catalog feature whose parts are all among its
-- subjects, compared without case. That rebuilds a catalog work's vector
-- to a cosine of about 0.5 (checked on the works that have both), which is
-- good enough for a seed with no description to compare.
CREATE OR REPLACE TABLE genre_vector AS
WITH heading AS (
    SELECT DISTINCT
        wr.work_id,
        s.scheme,
        CASE WHEN s.scheme = 'bisac' THEN string_split(lower(s.name), ' / ') ELSE [lower(s.name)] END AS parts
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id,
         unnest(ct.subjects) AS u(s)
),
catalog_feature AS (  -- each heading and those above it
    SELECT DISTINCT
        work_id,
        scheme || ':' || array_to_string(parts[1:depth], ' / ') AS feature,
        parts[1:depth] AS parts
    FROM heading, generate_series(1, len(parts)) AS g(depth)
),
n AS (
    SELECT count(DISTINCT work_id) AS works FROM catalog_feature
),
idf AS (
    SELECT feature, any_value(parts) AS parts, ln(n.works / count(*)) AS idf
    FROM catalog_feature, n
    GROUP BY feature, n.works
    HAVING count(*) <= n.works / 4
),
shelf_subjects AS (  -- works with no catalog subjects
    SELECT wr.work_id, list(DISTINCT lower(trim(u.s))) AS subjects
    FROM work_record wr
    JOIN shelf_item si ON wr.ref = si.source || ':' || si.item_id,
         unnest(si.subjects) AS u(s)
    WHERE wr.work_id NOT IN (SELECT work_id FROM catalog_feature)
    GROUP BY wr.work_id
),
feature AS (
    SELECT work_id, feature FROM catalog_feature
    UNION ALL
    SELECT ss.work_id, i.feature
    FROM shelf_subjects ss
    JOIN idf i ON list_has_all(ss.subjects, i.parts)
),
weighted AS (
    SELECT f.work_id, f.feature, i.idf
    FROM feature f
    JOIN idf i USING (feature)
)
SELECT work_id, feature, idf / sqrt(sum(idf * idf) OVER (PARTITION BY work_id)) AS weight
FROM weighted;
