-- The contract tables. Sources write here and nothing else does.
--
-- Each table mirrors a type in internal/contract; that package documents
-- what the columns mean. Derived tables (works, signals, ...) are rebuilt
-- from these by `recbook build`, from SQL kept next to the Go that runs it.
--
-- The CHECK constraints repeat the contract's vocabularies as a backstop;
-- store_test.go fails if the two drift apart.

-- One row per contract.ShelfItem.
CREATE TABLE shelf_item (
    source       VARCHAR NOT NULL,  -- the adapter that wrote it: 'readinglist', 'libby', ...
    item_id      VARCHAR NOT NULL,
    title        VARCHAR NOT NULL,
    subtitle     VARCHAR,
    authors      VARCHAR[] NOT NULL,
    isbns        VARCHAR[] NOT NULL,  -- always ISBN-13; the store converts
    format       VARCHAR NOT NULL CHECK (format IN ('print', 'ebook', 'audiobook', 'unknown')),
    refs         MAP(VARCHAR, VARCHAR) NOT NULL,
    description  VARCHAR,
    subjects     VARCHAR[] NOT NULL,
    series       VARCHAR,
    series_index VARCHAR,
    language     VARCHAR,
    pub_year     INTEGER,
    pages        INTEGER,
    PRIMARY KEY (source, item_id)
);

-- One row per contract.ShelfEvent.
CREATE TABLE shelf_event (
    source       VARCHAR NOT NULL,
    item_id      VARCHAR NOT NULL,
    kind         VARCHAR NOT NULL CHECK (kind IN (
                     'wanted', 'started', 'finished', 'abandoned', 'acquired',
                     'hold_placed', 'borrowed', 'renewed', 'returned')),
    happened_on  DATE,  -- NULL when the source doesn't know
    date_quality VARCHAR NOT NULL CHECK (date_quality IN ('system', 'user', 'missing')),
    detail       VARCHAR,
    CHECK ((happened_on IS NULL) = (date_quality = 'missing'))
);

-- The latest ingest of each source.
CREATE TABLE shelf_import (
    source      VARCHAR PRIMARY KEY,
    file        VARCHAR NOT NULL,
    imported_at TIMESTAMP NOT NULL,
    items       INTEGER NOT NULL,
    events      INTEGER NOT NULL
);

-- One row per contract.CatalogTitle, per library.
CREATE TABLE catalog_title (
    library       VARCHAR NOT NULL,  -- library key, as in libbyapp.com/library/{key}
    title_id      VARCHAR NOT NULL,
    title         VARCHAR NOT NULL,
    subtitle      VARCHAR,
    creators      STRUCT(name VARCHAR, role VARCHAR)[] NOT NULL,
    isbns         VARCHAR[] NOT NULL,
    format        VARCHAR NOT NULL CHECK (format IN ('print', 'ebook', 'audiobook', 'unknown')),
    edition       VARCHAR,
    publisher     VARCHAR,
    published_on  DATE,
    language      VARCHAR,
    description   VARCHAR,  -- raw, HTML included
    subjects      STRUCT(scheme VARCHAR, code VARCHAR, name VARCHAR)[] NOT NULL,
    series        VARCHAR,
    series_index  VARCHAR,
    duration_min  INTEGER,
    refs          MAP(VARCHAR, VARCHAR) NOT NULL,
    cover_url     VARCHAR,
    url           VARCHAR,
    first_seen_at TIMESTAMP NOT NULL,  -- the first crawl that found it
    last_seen_at  TIMESTAMP NOT NULL,  -- the latest crawl that found it
    PRIMARY KEY (library, title_id)
);

-- One row per contract.Availability. Snapshots accumulate rather than
-- overwrite, so wait estimates can be checked against what happened.
CREATE TABLE availability (
    library          VARCHAR NOT NULL,
    title_id         VARCHAR NOT NULL,
    fetched_at       TIMESTAMP NOT NULL,
    available        BOOLEAN NOT NULL,
    copies_owned     INTEGER NOT NULL,
    copies_available INTEGER NOT NULL,
    holds            INTEGER NOT NULL,
    wait_days        INTEGER  -- NULL when the library gives no estimate
);

CREATE VIEW latest_availability AS
SELECT *
FROM availability
QUALIFY row_number() OVER (PARTITION BY library, title_id ORDER BY fetched_at DESC) = 1;

-- One row per catalog crawl. A title whose last_seen_at is older than the
-- latest finished crawl's started_at has left the collection.
CREATE TABLE crawl (
    library     VARCHAR NOT NULL,
    started_at  TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    titles      INTEGER
);
