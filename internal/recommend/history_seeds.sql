-- The seeds of a "for you" list: every book I've finished, and every book
-- I tried and gave up on (tier N), whose likes it pushes down. A loan that
-- just ran out isn't tier N (signals.sql), so it isn't here.
SELECT work_id, finished
FROM work_signal
WHERE finished OR tier = 'N'
ORDER BY work_id
