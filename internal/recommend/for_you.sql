-- for_you: the "for you" list made from my whole history, deep enough to
-- filter (BuildForYou), and for_you_group, the groups of seeds behind it.
-- Each result records its place, the seed it's closest to, and the group
-- that seed belongs to. Availability isn't kept: the list is read through
-- borrowable, so it's as fresh as the last refresh (for_you_list.sql).
CREATE OR REPLACE TABLE for_you (
    rank        INTEGER NOT NULL,
    work_id     BIGINT NOT NULL,
    "like"      VARCHAR NOT NULL,  -- the closest seed's title: the "why"
    similarity  DOUBLE NOT NULL,   -- ...its similarity
    view        VARCHAR NOT NULL,  -- ...in this view
    group_rank  INTEGER NOT NULL   -- the group whose turn it filled
);
CREATE OR REPLACE TABLE for_you_group (
    rank    INTEGER NOT NULL,      -- heaviest first
    leader  BIGINT NOT NULL,       -- the seed it formed around
    title   VARCHAR NOT NULL,
    author  VARCHAR NOT NULL,
    seeds   INTEGER NOT NULL,
    weight  DOUBLE NOT NULL
);
