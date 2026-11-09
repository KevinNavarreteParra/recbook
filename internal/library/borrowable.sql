-- borrowable: one row per work the library has as an audiobook, holding
-- the single recording to offer and its availability as of the last crawl.
-- The full text beats an abridged or dramatized version even if it means a
-- wait; then available now, the shortest wait, the longest running time.
CREATE OR REPLACE TABLE borrowable AS
WITH recording AS (
    SELECT
        wr.work_id,
        ct.library, ct.title_id,
        ct.title, ct.subtitle,
        coalesce(list_filter(ct.creators, lambda c: c.role = 'author')[1].name, ct.creators[1].name) AS author,
        list_filter(ct.creators, lambda c: c.role = 'narrator')[1].name AS narrator,
        ct.duration_min, ct.url,
        coalesce(a.available, false) AS available,
        a.wait_days, a.holds,
        -- Not the whole book as written. Labels seen in a real catalog:
        -- "Abridged", "Dramatized"/"Dramatised", "Adapted", "Audio Theater",
        -- "Unabridged Selections", "Unabridged, Young Readers".
        coalesce(
            (ct.edition ILIKE '%abridged%' AND ct.edition NOT ILIKE '%unabridged%')
            OR regexp_matches(ct.edition, '(?i)dramati|adapt|audio theat|selections|young readers'),
            false) AS abridged
    FROM work_record wr
    JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id
    LEFT JOIN latest_availability a ON a.library = ct.library AND a.title_id = ct.title_id
    WHERE ct.format = 'audiobook'
)
SELECT *, count(*) OVER (PARTITION BY work_id) AS recordings
FROM recording
QUALIFY row_number() OVER (
    PARTITION BY work_id
    ORDER BY abridged, available DESC, wait_days NULLS LAST, duration_min DESC NULLS LAST, title_id) = 1;
