-- work_vector: each work's content vector, per model: its text in
-- work_text, looked up in the embedding cache. Queries compare a seed with
-- every borrowable work's vector, and joining work_text to the embedding
-- cache for each of them took ten times as long as the comparing (about
-- 1.9 s against 0.2 s a query, copying 300 MB of vectors every time).
CREATE OR REPLACE TABLE work_vector AS
SELECT t.work_id, e.model, e.vector
FROM work_text t
JOIN embedding e USING (text_hash)
ORDER BY e.model, t.work_id;
