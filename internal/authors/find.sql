-- Authors whose name has every word of the query ($q is a textkey.Person
-- key, so accents, punctuation and word order don't matter). The author
-- with exactly that name comes first, then the ones I've read, then the
-- ones on my shelf, then the most prolific in the catalog.
SELECT author_id, name, works, in_catalog, on_shelf, finished
FROM author
WHERE list_has_all(string_split(author_id, ' '), string_split($q, ' '))
ORDER BY author_id = $q DESC, finished DESC, on_shelf DESC, in_catalog DESC, author_id
LIMIT 5
