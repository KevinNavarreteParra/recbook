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
go run ./cmd/recbook crawl --library lasvegas        # the key in libbyapp.com/library/<key>; re-crawls fetch only new titles in full
go run ./cmd/recbook enrich                          # describe finished books no source describes
go run ./cmd/recbook build
go run ./cmd/recbook embed                           # on the GPU; the nightly job keeps it current

go run ./cmd/recbook search cold war berlin --available-now
go run ./cmd/recbook similar --seed "tinker tailor" --seed "red october" --max-wait 14
go run ./cmd/recbook backlog --available-now

go run ./cmd/recbook eval                            # how well it ranks books I finished
```

Personal data lives in `data/` (gitignored). Hand corrections to matching go
in `data/match_overrides.csv`, and `build` lists matches worth checking in
`data/review/matches.csv`. There is no CI workflow: run `make check` before
pushing.

A finished book with no description can't be a seed. `enrich` looks those
up in Google Books, then Open Library, and saves what it finds as the shelf
`enrich`; every answer is cached, so each book is asked about once. Google
Books usually refuses requests without an API key, which can be given in
`RECBOOK_GOOGLE_BOOKS_KEY`.

## Measuring it

`recbook eval` holds out each finished book the library lends, as if I
hadn't read it, and asks where recommendations would rank it. It asks in
two ways: with every other finished book as a seed at once (*history*, a
"for you" list), and with the best single one of them (*one seed*, query
by example). Neither lets a similarity view choose the seeds, so views can
be compared fairly. Each is scored against books by authors I've finished,
books on the shelves I read most from, and chance, and every book's ranks
go to `data/review/loo.csv` for comparing runs. Run it before and after a
change to the ranking.

## Web app

`recbook serve` runs a private web app on 127.0.0.1:8484. It has search as
you type, recommendations from seeds or "more like this", the backlog, and a
tracker with listening / finished / gave up / want buttons on every book.
It reads the copy of the database that `build`, `embed` and the nightly job
publish, and writes only `data/tracker.jsonl`, which the next build loads
like any other source. `make web-install` runs it as a sandboxed systemd
user service. To reach it over Tailscale, run
`tailscale serve --bg --http=8484 http://127.0.0.1:8484` as the Tailscale
operator.

## Nightly job

`recbook nightly` re-crawls the catalog once a week, and on the other
nights refreshes availability for the backlog alone (`refresh`: every
recording of a book I own or want, two requests for ~175 titles). It then
looks up descriptions for newly finished books (`enrich`), rebuilds, and
embeds any new descriptions on the GPU. A systemd user timer starts it at 02:00
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
| `eval` | leave-one-out over finished books, against baselines |
| `backlog` | owned or wanted books the library can lend |
| `enrich` | descriptions from Google Books and Open Library for finished books |
| `polite` | the rate-limited, retrying HTTP client every online source uses |
| `blurb`, `embed`, `ollama` | clean descriptions, embed them, run the local model |
| `nightly` | the night window and GPU guard |
| `web` | the web app and its tracker |
| `demo` | synthetic data for tests and `recbook demo` |
| `cli` | one file per command |
