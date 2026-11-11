-- content_mean: the mean of every work's vector, per model. The content
-- vectors share a large common part (this mean's length is about 0.8 for
-- nomic-embed-text), and an author's mean is mostly that part, more so the
-- more varied their books. Measured from it, every prolific author looks
-- like every other: Stephen King and Ken Follett came out among Kafka's
-- nearest. Subtracting it first (spec §8, "center each view") leaves what
-- sets an author apart, and Kafka's nearest become Camus, Hesse and Eco.
CREATE OR REPLACE TABLE content_mean AS
SELECT model, list(mean ORDER BY i)::FLOAT[] AS vector
FROM (
    -- one row per dimension: its mean over the works
    SELECT model, i, avg(x) AS mean
    FROM (
        SELECT e.model, unnest(e.vector) AS x, generate_subscripts(e.vector, 1) AS i
        FROM (SELECT DISTINCT text_hash FROM work_text) t
        JOIN embedding e USING (text_hash)
    )
    GROUP BY model, i
)
GROUP BY model;

-- author_vector: each author's typical territory in the content view (spec
-- §6): the mean of their works' vectors, less content_mean, scaled to unit
-- length so that an inner product with it is a cosine. works counts the
-- works averaged: a mean of one or two books is a much noisier picture of
-- an author than a mean of twenty.
CREATE OR REPLACE TABLE author_vector AS
WITH vectors AS (
    SELECT aw.author_id, e.model, list(e.vector) AS vectors
    FROM author_work aw
    JOIN work_text t USING (work_id)
    JOIN embedding e ON e.text_hash = t.text_hash
    GROUP BY aw.author_id, e.model
),
centred AS (
    SELECT v.author_id, v.model, len(v.vectors) AS works,
           list_transform(range(1, len(c.vector) + 1),
                          lambda i: list_avg(list_transform(v.vectors, lambda w: w[i])) - c.vector[i]) AS v
    FROM vectors v
    JOIN content_mean c USING (model)
)
SELECT author_id, model, works,
       list_transform(v, lambda x: x / sqrt(list_inner_product(v, v)))::FLOAT[] AS vector
FROM centred;
