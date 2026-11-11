-- description_lookup: what Google Books and Open Library answered when
-- asked for a book's description (internal/enrich). Like embedding, this is
-- a cache of slow work, never rebuilt and only added to. Each question is
-- asked once, and "found nothing" is kept too, so it isn't asked again.
CREATE TABLE description_lookup (
    service     VARCHAR NOT NULL,  -- the ref key for its ids: 'google_books' or 'openlibrary_work'
    query       VARCHAR NOT NULL,  -- 'isbn:9780…' or 'title:<title key>|author:<author key>'
    asked_at    TIMESTAMP NOT NULL,
    book_id     VARCHAR,           -- the service's id for what it found; NULL when nothing
    title       VARCHAR,           -- what it found, as it names it
    description VARCHAR,           -- raw; may be HTML
    PRIMARY KEY (service, query)
);
