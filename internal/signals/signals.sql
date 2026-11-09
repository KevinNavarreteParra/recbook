-- tier: how strongly a work says "my taste" (spec §10). The weights are
-- guesses until leave-one-out evaluation calibrates them; change them here.
--
-- The spec's T1 (finished, then bought afterward) is folded into T2: it
-- needs a trustworthy purchase date, and almost none survive batch
-- detection. Tier names keep the spec's numbering.
CREATE OR REPLACE TABLE tier AS
SELECT * FROM (VALUES
    ('T2', 'owned and finished',            1.25),
    ('T3', 'finished, not owned',           1.00),
    ('T4', 'owned, not finished',           0.50),
    ('T5', 'wanted, not owned or finished', 0.50),
    ('N',  'tried and gave up',            -1.00)
) AS t(tier, meaning, seed_weight);

-- work_signal: what my history says about each work it touches. Nothing
-- here depends on a typed-in date; the only date used is a loan's start,
-- which Libby records itself.
CREATE OR REPLACE TABLE work_signal AS
WITH facts AS (
    SELECT
        work_id,
        bool_or(kind = 'finished')                      AS finished,
        bool_or(kind = 'acquired')                      AS owned,
        bool_or(kind = 'wanted')                        AS wanted,
        bool_or(kind = 'abandoned')                     AS abandoned,
        count(*) FILTER (kind = 'borrowed')             AS borrows,
        count(*) FILTER (kind = 'renewed')              AS renewals,
        bool_or(kind = 'returned' AND detail = 'early') AS returned_early,
        -- The loan has ended: a return was recorded, or the latest borrow is
        -- older than a loan lasts. Libby records no returns at all, so for
        -- its loans the second test is the only one that applies.
        bool_or(kind = 'returned')
            OR date_diff('day', max(happened_on) FILTER (kind = 'borrowed'), current_date)
               > getvariable('loan_days')               AS loan_over
    FROM history
    GROUP BY work_id
),
judged AS (
    SELECT
        *,

        -- The label: 1 if the book held my attention, 0 if it didn't.
        CASE
            WHEN finished THEN 1
            WHEN abandoned OR returned_early THEN 0
            WHEN borrows = 1 AND renewals = 0 AND loan_over THEN 0
            -- Renewed or re-borrowed but never finished is ambiguous, and a
            -- loan still running has no outcome yet: no label.
        END AS label,
        CASE
            WHEN finished OR abandoned OR returned_early THEN 'high'
            -- Low: the loan may just have run out on me.
            WHEN borrows = 1 AND renewals = 0 AND loan_over THEN 'low'
        END AS label_confidence,

        -- The tier: ownership × finishing.
        CASE
            WHEN finished AND owned THEN 'T2'
            WHEN finished THEN 'T3'
            -- Before T4: trying it and quitting says more than having bought it.
            WHEN abandoned OR returned_early THEN 'N'
            WHEN owned THEN 'T4'
            WHEN wanted THEN 'T5'
        END AS tier
    FROM facts
)
SELECT j.*, t.seed_weight
FROM judged j
LEFT JOIN tier t USING (tier);
