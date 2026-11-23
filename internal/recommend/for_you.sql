-- for_you: the "for you" list made from my whole history, deep enough to
-- filter (BuildForYou), each result with the finished book nearest it in
-- the content view, its "why"; and for_you_seed, the books it was made
-- from. Availability isn't kept: the list is read through borrowable, so
-- it's as fresh as the last refresh (for_you_list.sql).
CREATE OR REPLACE TABLE for_you (
    rank        INTEGER NOT NULL,
    work_id     BIGINT NOT NULL,
    "like"      VARCHAR NOT NULL,  -- the nearest finished book's title, or ''
    similarity  DOUBLE NOT NULL    -- ...and their cosine
);
CREATE OR REPLACE TABLE for_you_seed (
    work_id  BIGINT NOT NULL,
    title    VARCHAR NOT NULL,
    author   VARCHAR NOT NULL,
    weight   DOUBLE NOT NULL,
    gave_up  BOOLEAN NOT NULL      -- a book I gave up on, walked away from
);
