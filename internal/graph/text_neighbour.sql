-- text_neighbour: each embedded text's nearest texts in the content view,
-- by cosine, for the graph's work-to-work edges (BuildNeighbours). Keyed by
-- text hash, like the embedding cache, so builds don't invalidate it.
CREATE OR REPLACE TABLE text_neighbour (
    model       VARCHAR NOT NULL,
    text_hash   VARCHAR NOT NULL,
    neighbour   VARCHAR NOT NULL,  -- another text's hash
    similarity  DOUBLE NOT NULL
);
