-- The catalog's subjects of each of the works, by name: OverDrive's
-- ("Thriller") and BISAC headings ("Fiction / Thrillers / Espionage").
SELECT DISTINCT wr.work_id, s.name
FROM work_record wr
JOIN catalog_title ct ON wr.ref = 'catalog:' || ct.library || ':' || ct.title_id,
     unnest(ct.subjects) AS u(s)
WHERE list_contains($works::BIGINT[], wr.work_id)
ORDER BY wr.work_id, s.name
