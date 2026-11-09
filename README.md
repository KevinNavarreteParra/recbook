# recbook

Finds audiobooks I can borrow from my library that are like books I already
liked. It reads my reading history (what I finished, own, borrowed or gave
up on), crawls the library's OverDrive audiobook catalog, and matches the
two. The design is in [docs/spec.md](docs/spec.md); this repo builds it in
Go and DuckDB rather than the spec's Python and SQLite.

## How it fits together

```
exports (ReadingList, Libby, ...) ─► internal/sources/* ─┐
OverDrive Thunder API ─► internal/sources/overdrive ─────┴─► contract ─► DuckDB
                                                                           │
                       recbook build:  resolve (records → works)  ◄────────┘
                                       signals (dates, labels, tiers)
                                                   │
                       queries:        backlog (later: similarity search)
```

- **The contract** ([internal/contract](internal/contract)) is the canonical
  data the app runs on. Every source is an adapter that produces it, and
  nothing downstream reads a raw export. Start reading here.
- **`build`** rebuilds every derived table from the contract tables. SQL does
  the data work. Each package keeps its `.sql` beside the Go that runs it,
  and Go does I/O and algorithms (union-find now; graph ranking and
  diversification later).

## Usage

Needs Go 1.27+ and a C compiler (the DuckDB driver uses cgo).

```sh
go run ./cmd/recbook demo                            # made-up data in data/demo.duckdb
go run ./cmd/recbook --db data/demo.duckdb backlog

go run ./cmd/recbook ingest jsonl --as goodreads my-export.jsonl
go run ./cmd/recbook crawl --library <key>           # <key> from libbyapp.com/library/<key>
go run ./cmd/recbook build
go run ./cmd/recbook backlog --available-now
```

Personal data lives in `data/` (gitignored). Hand corrections to matching go
in `data/match_overrides.csv`, and `build` lists matches worth checking in
`data/review/matches.csv`. There is no CI workflow: run `make check` before
pushing.

## Adding a source

Add a package under `internal/sources/` whose reader turns the export into
a `contract.Shelf`, and list it in `internal/sources/sources.go`. To skip
writing Go, have any script emit the JSONL form of the contract
(`internal/sources/jsonl`) and run `recbook ingest jsonl --as <name>`. Either
way, the store rejects a shelf that breaks the contract and lists every
problem.

## Layout

| Package | Does |
|---|---|
| `contract` | canonical records and their validation |
| `sources/jsonl`, `sources/overdrive` | adapters |
| `store` | DuckDB schema, migrations, writes |
| `textkey` | title and author matching keys |
| `resolve` | matches records into works |
| `signals` | trusted dates, labels, provenance, signal tiers |
| `backlog` | owned or wanted books the library can lend |
| `demo` | synthetic data for tests and `recbook demo` |
| `cli` | one file per command |
