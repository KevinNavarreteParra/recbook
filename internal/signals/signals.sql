-- tier: how strongly a work says "my taste" (spec §10). The weights are
-- guesses until leave-one-out evaluation calibrates them; change them here.
CREATE OR REPLACE TABLE tier AS
SELECT * FROM (VALUES
    ('T1', 'finished, then bought a copy afterward', 1.50),
    ('T2', 'owned and finished, order unknown',      1.25),
    ('T3', 'finished, not owned',                    1.00),
    ('T4', 'owned, not finished',                    0.50),
    ('T5', 'wanted, not owned or finished',          0.50),
    ('N',  'tried and gave up',                     -1.00)
) AS t(tier, meaning, seed_weight);

-- work_signal: what my history says about each work it touches.
--
-- "A before B" needs a gap of at least order_gap_days between trusted
-- dates; anything closer, or any untrusted date, means the order is unknown.
CREATE OR REPLACE TABLE work_signal AS
WITH facts AS (
    SELECT
        work_id,
        bool_or(kind = 'finished')                                   AS finished,
        bool_or(kind = 'acquired')                                   AS owned,
        bool_or(kind = 'wanted')                                     AS wanted,
        bool_or(kind = 'abandoned')                                  AS abandoned,
        count(*) FILTER (kind = 'borrowed')                          AS borrows,
        count(*) FILTER (kind = 'renewed')                           AS renewals,
        bool_or(kind = 'returned' AND detail = 'early')              AS returned_early,
        -- The loan has ended: a return was recorded, or the latest borrow is
        -- older than a loan lasts. Libby records no returns at all, so for
        -- its loans the second test is the only one that applies.
        bool_or(kind = 'returned')
            OR date_diff('day', max(happened_on) FILTER (kind = 'borrowed'), current_date)
               > getvariable('loan_days')                            AS loan_over,
        min(trusted_on) FILTER (kind = 'finished')                   AS finished_on,
        min(trusted_on) FILTER (kind = 'acquired')                   AS acquired_on,
        min(trusted_on) FILTER (kind = 'wanted')                     AS wanted_on,
        min(trusted_on) FILTER (kind = 'borrowed')                   AS first_borrowed_on
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

        -- Provenance: was it borrowed off the to-read list, or found while
        -- browsing? Only dated wanted events can say. No wanted event is not
        -- evidence of browsing, because a reading app may forget a book was
        -- on its to-read list once it's started (Reading List does).
        CASE
            WHEN borrows = 0 THEN NULL
            WHEN wanted_on IS NULL OR first_borrowed_on IS NULL THEN 'unknown'
            WHEN date_diff('day', wanted_on, first_borrowed_on) >= getvariable('order_gap_days') THEN 'to_read_list'
            WHEN date_diff('day', first_borrowed_on, wanted_on) >= getvariable('order_gap_days') THEN 'search'
            ELSE 'unknown'  -- listed around when I borrowed it: maybe found in search and added on the spot
        END AS provenance,

        -- The tier: ownership × finishing × the order of the two.
        CASE
            WHEN finished AND owned
                 AND date_diff('day', finished_on, acquired_on) >= getvariable('order_gap_days') THEN 'T1'
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
