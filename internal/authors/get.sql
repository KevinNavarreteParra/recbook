-- One author, and how many works their vector in $model averages (0 when
-- none of their books has been embedded).
SELECT a.author_id, a.name, a.works, a.in_catalog, a.on_shelf, a.finished, coalesce(v.works, 0)
FROM author a
LEFT JOIN author_vector v ON v.author_id = a.author_id AND v.model = $model
WHERE a.author_id = $author
