-- record: everything that might be a book, from both the shelf and the
-- catalog, in one shape. ref is how a record is named everywhere downstream,
-- including match_overrides.csv:
--
--   '<source>:<item_id>'            a shelf item       readinglist:1234
--   'catalog:<library>:<title_id>'  a catalog title    catalog:lapl:5700929
CREATE OR REPLACE TABLE record AS
SELECT
    source || ':' || item_id AS ref,
    'shelf'                  AS side,
    title,
    subtitle,
    authors[1]               AS author,
    isbns,
    refs,
    format
FROM shelf_item
UNION ALL
SELECT
    'catalog:' || library || ':' || title_id,
    'catalog',
    title,
    subtitle,
    -- the first credited author, or whoever is credited first if no one is
    coalesce(list_filter(creators, lambda c: c.role = 'author')[1].name, creators[1].name),
    isbns,
    refs,
    format
FROM catalog_title;

-- record_key: each record's matching keys, filled in by Go (internal/textkey).
CREATE OR REPLACE TABLE record_key (
    ref        VARCHAR PRIMARY KEY,
    title_key  VARCHAR NOT NULL,  -- whole title, subtitle included
    main_key   VARCHAR NOT NULL,  -- the part before the colon
    author_key VARCHAR NOT NULL   -- '' when unknown or a placeholder like "Anonymous"
);

-- work_record: which work each record belongs to, filled in by Go from the
-- match_edge clusters.
CREATE OR REPLACE TABLE work_record (
    work_id BIGINT NOT NULL,
    ref     VARCHAR NOT NULL
);
