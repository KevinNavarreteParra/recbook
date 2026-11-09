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
                       queries:        search, similar, backlog
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

go run ./cmd/recbook ingest readinglist  data/exports/readinglist.csv
go run ./cmd/recbook ingest libby        data/exports/libby-timeline.json
go run ./cmd/recbook ingest librarything data/exports/librarything.json
go run ./cmd/recbook crawl --library lasvegas        # the key in libbyapp.com/library/<key>
go run ./cmd/recbook build
go run ./cmd/recbook embed                           # on the GPU; the nightly job keeps it current

go run ./cmd/recbook search cold war berlin --available-now
go run ./cmd/recbook similar --seed "tinker tailor" --seed "red october" --max-wait 14
go run ./cmd/recbook backlog --available-now
```

Personal data lives in `data/` (gitignored). Hand corrections to matching go
in `data/match_overrides.csv`, and `build` lists matches worth checking in
`data/review/matches.csv`. There is no CI workflow: run `make check` before
pushing.

## Nightly job

`recbook nightly` re-crawls the catalog once a week, rebuilds, and embeds
any new descriptions on the GPU. A systemd user timer starts it at 02:00
(`make nightly-install`; units in `deploy/systemd`). It does nothing outside
02:00-06:00 and stops when the window closes. It won't start while the GPU
is busy and backs off if a game starts. Ollama runs only for the length of
the job, and whatever a night doesn't finish, the next night continues.
Logs: `journalctl --user -u recbook-nightly`.

## Adding a source

Add a package under `internal/sources/` whose reader turns the export into
a `contract.Shelf`, and list it in `internal/sources/sources.go`. To skip
writing Go, have any script emit the JSONL form of the contract
(`internal/sources/jsonl`) and run `recbook ingest jsonl --as goodreads
file.jsonl`. Either way, the store rejects a shelf that breaks the contract
and lists every problem.

## Layout

| Package | Does |
|---|---|
| `contract` | canonical records and their validation |
| `sources/*` | adapters: `readinglist`, `libby`, `librarything`, `overdrive`, and `jsonl` |
| `store` | DuckDB schema, migrations, writes |
| `textkey` | title and author matching keys |
| `resolve` | matches records into works |
| `signals` | trusted dates, labels, provenance, signal tiers |
| `library` | the one recording of each work to offer, and its availability |
| `search` | keyword (BM25) search, and finding works by name |
| `recommend` | books like a set of seeds, from the content vectors |
| `backlog` | owned or wanted books the library can lend |
| `blurb`, `embed`, `ollama` | clean descriptions, embed them, run the local model |
| `nightly` | the night window and GPU guard |
| `demo` | synthetic data for tests and `recbook demo` |
| `cli` | one file per command |
