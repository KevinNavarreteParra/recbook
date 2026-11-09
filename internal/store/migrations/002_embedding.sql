-- embedding: vectors computed by a local model. Unlike the derived tables,
-- this is a cache of slow work and is never rebuilt. Rows are keyed by the
-- exact text embedded rather than by work, so they survive rebuilds (which
-- renumber works), a text is embedded once however many works share it,
-- and an interrupted run picks up where it stopped.
CREATE TABLE embedding (
    model       VARCHAR NOT NULL,  -- e.g. 'nomic-embed-text'
    text_hash   VARCHAR NOT NULL,  -- see internal/embed
    vector      FLOAT[] NOT NULL,  -- unit length
    embedded_at TIMESTAMP NOT NULL,
    PRIMARY KEY (model, text_hash)
);
