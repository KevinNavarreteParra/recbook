-- The content vectors of some works, for diversifying a list of them.
SELECT work_id, vector
FROM work_vector
WHERE model = $model AND work_id IN (SELECT unnest($ids::BIGINT[]))
