Synthetic demo data in the JSONL contract format (internal/sources/jsonl).
Three made-up sources play the roles of a reading app, an owned-books
catalog and a library's loan history. The ISBNs are valid but invented.
Tests use these files; `recbook ingest jsonl --as <name> <file>` loads them.
