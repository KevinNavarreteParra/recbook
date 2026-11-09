-- work_text: the exact text embedded for each work, rebuilt on every embed
-- run (work ids change with every build). Join it to embedding on
-- text_hash to get a work's vector.
CREATE OR REPLACE TABLE work_text (
    work_id   BIGINT  NOT NULL,
    side      VARCHAR NOT NULL,  -- where the description came from: catalog or shelf
    text      VARCHAR NOT NULL,  -- title, then the cleaned blurb
    text_hash VARCHAR NOT NULL,
    thin      BOOLEAN NOT NULL   -- the blurb was too short to say much
);
