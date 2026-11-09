-- history: every shelf event attached to its work, with its date judged
-- by the rules in spec §2.
--
-- quality is the source's date_quality plus one value sources never
-- report: user_batch, for a typed-in date shared by at least batch_min
-- events of one kind from one source. Those are the day of a catch-up
-- session, not the day anything happened.
--
-- trusted_on is the date when it can be used for ordering (system or user
-- quality), and NULL otherwise. Nothing ranks on typed-in dates today; the
-- judged dates are here so questions about them can be asked of the data.
CREATE OR REPLACE TABLE history AS
WITH e AS (
    SELECT wr.work_id, ev.source, ev.item_id, ev.kind, ev.happened_on, ev.date_quality, ev.detail
    FROM shelf_event ev
    JOIN work_record wr ON wr.ref = ev.source || ':' || ev.item_id
),
batch_day AS (
    SELECT source, kind, happened_on
    FROM e
    WHERE date_quality = 'user'
    GROUP BY source, kind, happened_on
    HAVING count(*) >= getvariable('batch_min')
)
SELECT
    e.*,
    CASE WHEN b.happened_on IS NOT NULL THEN 'user_batch' ELSE e.date_quality END AS quality,
    CASE WHEN b.happened_on IS NULL AND e.date_quality IN ('system', 'user') THEN e.happened_on END AS trusted_on
FROM e
LEFT JOIN batch_day b USING (source, kind, happened_on);
