-- history.sql: what I've started, finished and given up on, by every
-- shelf source but the tracker (the page reads its marks from the file,
-- so they show at once): each book's latest such event from each source,
-- titled as its work is, newest first. A start and a finish on the same
-- day show as the finish.
SELECT h.source, h.happened_on, h.kind, w.title, coalesce(w.author, '')
FROM history h
JOIN work w USING (work_id)
WHERE h.source <> 'tracker'
  AND h.kind IN ('started', 'finished', 'abandoned')
  AND h.happened_on IS NOT NULL
QUALIFY row_number() OVER (
    PARTITION BY h.source, h.item_id
    ORDER BY h.happened_on DESC, h.kind = 'started'
) = 1
ORDER BY h.happened_on DESC, w.title;
