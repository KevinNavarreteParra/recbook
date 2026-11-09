# Libby Audiobook Recommender — Spec

*v0.1 · working draft*

## 1. Goal

Start from books I already know I liked, and find audiobooks **I can actually borrow on my library card** that are similar to them.

Primary interaction is **query by example**: pick one or more seed books (or an author), choose what "similar" means, and get back a ranked pool of borrowable audiobooks, with availability and wait time.

### Non-goals (for now)

- Automated borrowing or placing holds (that needs the authenticated Libby API; it's fragile and not worth it yet).
- A general-purpose recommender. This only has to work for one reader.
- Fine-grained rating prediction. The only label is binary: finished or not.

## 2. Core concepts

| Concept | Definition |
|---|---|
| **Search universe** | Every audiobook in my library's OverDrive collection. This is the only thing results come from. |
| **Seed universe** | Every book I've finished (ReadingList, ~400), Libby borrows I didn't finish (~100), and books I own (LibraryThing). Seeds can come from outside the search universe, such as print books the library doesn't carry. |
| **Signal tier** | How strongly a seed says "my taste." It combines ownership, finishing, and the order of those events (§10). |
| **Work** | One book regardless of edition or format. Recommendations are made at the work level, then resolved to a borrowable audiobook edition. |
| **View** | One definition of similarity (content, genre, author, …). A query picks one view or a weighted blend. |

### Date reliability (applies everywhere)

| Source | Dates | Trust |
|---|---|---|
| Libby timeline | borrowed / returned / renewed / hold | **Reliable.** The system records them. |
| ReadingList | added, started, finished | **Fuzzy.** Start and finish are usually logged on the same day, so reading duration is meaningless. A finish date is good to roughly a month at best. |
| LibraryThing | date entered, date acquired | **Fuzzy.** Books often get added in catch-up batches, so an entry date may say nothing about when the book was bought. |

Rules:

1. **Dates are never the main signal.** They refine tiers and break ties; the system must work with every user-entered date set to null.
2. **Never compute reading duration** from ReadingList start/end dates.
3. **Batch detection:** if ≥N books (start with N=5) share the same LibraryThing entry or acquired date, treat those dates as *unknown* for all books in the batch.
4. **Comparisons use a tolerance window:** "A before B" requires a gap of at least 30 days. Anything closer is "unknown order."
5. Store a `date_quality` flag (`system` / `user` / `user_batch` / `missing`) alongside each date.

Seeds and search results live in different universes, so **every seed needs its own representation** (description, subjects) even when the library doesn't carry it. That's the main data-engineering cost in the project.

## 3. Data sources

### 3.1 Library catalog — OverDrive Thunder API (public, no auth)

- Base URL: `https://thunder.api.overdrive.com/v2/libraries/{libraryKey}/media`
- `libraryKey` = the slug in the `libbyapp.com/library/{key}` URL.
- Crawl: page through with the audiobook format filter (`format=audiobook-overdrive`, plus `audiobook-mp3` if the library has it) and a large `perPage`.
- Fields to capture (confirm the exact names in browser devtools): OverDrive id, title, subtitle, creators with roles (author / narrator), series and series position, publisher, publish date, subjects, BISAC codes (if present), description (HTML), language, duration, cover URL, availability (`isAvailable`, `availableCopies`, `ownedCopies`, `holdsCount`, `estimatedWaitDays`).
- **Two refresh cadences:**
  - Catalog metadata: weekly full crawl, diffed (new titles, dropped titles).
  - Availability: nightly, or on demand for the result set only.
- Rate limit: ≤2 requests/second, with a descriptive User-Agent, plus caching and retries with backoff.

> **Verify:** whether an empty query plus a format filter pages through the whole collection, or whether the crawl needs to go subject by subject. Check how large `perPage` can be. Check the collection size.

### 3.2 ReadingList export (CSV)

- Finished list, to-read list, date added, date finished, ISBN (where present), author, title, description (if exported).
- **Verify:** the actual column names. Whether "abandoned" or "currently reading" states exist.

### 3.3 Libby timeline export (CSV / JSON)

- Shelf → Timeline → Actions → Export Timeline.
- Events: borrowed, hold placed, renewed, returned (early vs. expired?).
- **Verify:** fields per event (title id? ISBN? format?). Whether the per-title "Reading Journey" export includes listening progress.

### 3.4 LibraryThing export (owned books)

- Export from More → Import/Export (CSV, tab-delimited, or JSON). Prefer **JSON**: it carries the most fields.
- Useful fields (per the export field list; verify against the real file): `workcode` (LibraryThing's cross-edition work id), `isbn`, `originalisbn`, `dateacquired`, `datestarted`, `dateread`, `format`, `ddc`, `lcc`, `subject`, `genre`, `summary`, `tags`, `collections`, `series`, `pages`.
- What this source adds:
  - **An interest signal:** I paid for the book, so I wanted it.
  - **Sequence:** `dateacquired` compared with the ReadingList finish date tells whether I bought the book before reading it or *after* finishing it.
  - **Better seed features:** DDC/LCC classification, subject headings, and summaries for print books. Much of the §3.5 enrichment can be skipped for owned books.
  - **Better entity resolution:** print ISBNs match ReadingList's ISBNs, and `workcode` groups editions.

> **Verify:** how consistently `dateacquired` is filled in. Whether `format` separates physical books from ebooks. Whether I use tags or collections in any meaningful way.

### 3.5 Descriptions and other text (embedding input)

Goal: a paragraph or more of text for **every** work, on both the catalog side and the seed side. Only metadata-level text: descriptions, summaries, encyclopedia entries. **No full texts of copyrighted books.**

#### Sources

| Source | Kind | Coverage | Access | Notes |
|---|---|---|---|---|
| OverDrive (Thunder `description`) | publisher blurb | every catalog title | already in the crawl | The search side is covered for free. |
| Google Books API | publisher blurb + categories | broad, recent commercial titles | free API key, daily quota | **Primary for seeds the library lacks.** Usually the same blurb style as OverDrive, so the two sides stay comparable (see symmetry below). |
| LibraryThing export `summary` | short summary | owned books | already exported | Often thin (one line). A fallback, not the main text. |
| Open Library | description + subjects | uneven; good for older books | free, no key | Subjects are useful even when the description is missing. |
| Wikipedia (REST API) | plot / overview section | notable books and classics | free, CC BY-SA | Much richer than a blurb, but a different kind of text. Keep it in its own view (below). |
| Hardcover API | description, genres, moods | growing | free token (GraphQL) | Optional; its "mood" tags could seed the `tone` view. |
| Project Gutenberg | **full text**, public domain only | pre-1929 works, older translations | free | **Off by default.** Used only by the `classics` query mode (§8). It would bias general queries: my seeds are heavy on classics and philosophy, so these works would carry far richer text than everything else. |

**Avoid:** scraping Goodreads or Amazon pages and reviews (against their terms, brittle, and the reviews are third-party copyrighted text), and any source of full copyrighted texts.

#### Storage

```
work_text (work_id, source, kind, text, char_len, fetched_at, license)
-- kind: blurb | summary | plot | excerpt
```

Keep every source's raw text; choose what to embed at embedding time. Cache every response: these lookups are one-time.

#### Cleaning (blurbs especially)

Publisher copy is noisy. Before embedding:

- Strip HTML, and remove audiobook boilerplate ("Narrated by…", "Includes a bonus PDF", "Unabridged").
- Remove **praise lines**: `"A masterpiece." — The New York Times`, "#1 bestselling author of…", award lists. These add the same signal to every bestseller and make unrelated books look similar.
- Remove series-marketing filler ("Don't miss the next thrilling installment…").
- Mark texts shorter than ~150 characters after cleaning as `thin`. For those, fall back to the next source, or lean on the `genre` view.

#### Symmetry (important)

Similarity scores are only meaningful when both sides are the **same kind of text**. A seed described by a Wikipedia plot summary and a catalog title described by a publisher blurb will score lower against each other than two blurbs about less similar books.

- The default `content` view is **blurb-to-blurb**. For seeds in the catalog, use the OverDrive blurb. For seeds the library lacks, use Google Books, then Open Library.
- **Optional normalization step:** have a local LLM rewrite whatever text exists into a fixed template (premise, setting, genre, tone, pace; ~120 words), then embed *that* for both sides. This removes the differences in style between sources and produces the `tone` tags as a side effect. Compare its leave-one-out score (§11) against raw blurbs before adopting it.
- `plot` view (Wikipedia) and `excerpt` view (Gutenberg) are separate views, compared only within their own kind of text, and only for works that have that text.

#### Full-text handling (`excerpt` view)

Full-book embeddings follow a different distribution from blurb embeddings. A single vector for a whole book is a mean over very long text, so it collapses toward a generic "literary prose" region, and every pair of classics looks moderately similar. To counter that:

- **Chunk, don't average.** Split each text into passages of ~500 tokens, skipping front matter, translator's notes, and Gutenberg license text. Embed each passage.
- **Score book-to-book by passage matching**, not by comparing mean vectors: the mean of each seed passage's top-k cosine similarities against the candidate's passages. This keeps distinctive sections (a Grand Inquisitor chapter, say) from being washed out.
- **Center per corpus:** subtract the mean `excerpt` vector before computing cosines. This removes the shared "old prose" direction that otherwise dominates. Optionally whiten as well (PCA, dropping the top 1–2 components).
- **Search universe for this view** = library audiobooks that match a Gutenberg work (public-domain classics in the catalog). The view can only recommend among those.

## 4. Data model (SQLite)

```
work            (work_id PK, norm_title, norm_primary_author, description, first_pub_year, source)
edition         (edition_id PK, work_id FK, overdrive_id, isbn, format, narrator, duration_min, publisher)
author          (author_id PK, norm_name, display_name)
work_author     (work_id, author_id, role)          -- role: author | narrator | translator
subject         (subject_id PK, name, scheme, idf)  -- scheme: overdrive | bisac | openlibrary | llm_tag
work_subject    (work_id, subject_id, weight)
series          (series_id PK, name)
work_series     (work_id, series_id, position)
availability    (edition_id, fetched_at, is_available, owned, available, holds, est_wait_days)
embedding       (work_id, view, model, dim, vector BLOB)

my_shelf        (work_id, status, date_added, date_finished, source)   -- status: finished | to_read
my_loans        (work_id, edition_id, borrowed_at, returned_at, end_reason, renewals, n_borrows)
my_owned        (work_id, lt_workcode, isbn, format, date_acquired, ddc, lcc, tags, collections)
my_labels       (work_id, label, label_confidence, provenance, signal_tier, seed_weight)   -- derived; see §10
```

`work` is the hub: catalog works and seed-only works share the table, and `source` tells them apart (`catalog`, `seed_only`, `both`).

## 5. Entity resolution

Needed in four places: catalog editions → work; ReadingList rows → work; Libby timeline rows → work; LibraryThing rows → work.

0. **Resolve the personal sources against each other first.** ReadingList ↔ LibraryThing is mostly print-to-print, so ISBN matching works well there. Use LibraryThing's `workcode` as the canonical work id for anything I own; then match the catalog against that merged set.
1. **Exact ISBN match** where both sides have one (rare for audio vs. print). Try LibraryThing's `originalisbn` as well as `isbn`.
2. **Normalized key:** lowercase, strip subtitle after `:`, strip leading articles, strip punctuation, ASCII-fold; primary author's last name plus first initial.
3. **Fuzzy fallback:** token-set similarity on title above a threshold, with exact author last-name match required. Log every fuzzy match to a review file.
4. **Manual overrides:** a `match_overrides.csv` (`source_id → work_id`, or `→ none`) that wins over everything else.

Known traps: omnibus editions, "A Novel" suffixes, translated titles, multiple narrators/editions of one work (e.g., two recordings of *Crime and Punishment*), and series numbering in titles.

## 6. Similarity views

Each view produces a vector or a set of edges per work.

| View | Representation | Captures | Notes |
|---|---|---|---|
| `content` | Text embedding of title + cleaned blurb (§3.5), or of the LLM-normalized summary | Plot, setting, premise, and (surprisingly well) genre and tone | Main view. Small local embedding model, normalized vectors. Blurb-to-blurb only. |
| `plot` *(optional)* | Embedding of the Wikipedia plot/overview section | Deeper content similarity for notable books | Sparse coverage. Compare plots only with other plots. |
| `genre` | IDF-weighted subject/BISAC vector (sparse) | Shelf category | Drop or heavily downweight near-universal subjects ("Fiction", "Literature"). |
| `author` | Mean `content` vector of the author's works, plus their subject profile | An author's typical territory | Powers author-to-author similarity. |
| `tone` *(later)* | LLM-extracted tags: pace, seriousness, literary vs. commercial, violence, humor, length feel | The "I want something dumb" axis | Local LLM over descriptions; tags stored as `subject` rows with scheme `llm_tag`. |
| `audio` *(later)* | Narrator id, duration bucket | Narrator affinity | Probably more useful as a reranker feature than a retrieval view. |

**Scale check:** if the audiobook collection is ~20–80k works, then at 384–768 dims, brute-force cosine in NumPy takes milliseconds. **No ANN index needed.** Precompute top-k neighbors per work offline anyway, for the graph.

## 7. Graph

A heterogeneous graph built from the tables, with nodes for `work`, `author`, `subject`, `series`, and `narrator`.

| Edge | Weight |
|---|---|
| work — author | 1.0 |
| work — series | high (series mates are the strongest "same itch" signal) |
| work — subject | subject IDF × assignment weight |
| work — narrator | low by default |
| work — work (kNN, `content` view) | cosine similarity, top-k (k≈15) per work, above a threshold |

**Traversal = personalized PageRank (PPR)** starting from the query's seed nodes, run as power iteration on a `scipy.sparse` matrix (no graph-database dependency).

- Book seeds: restart mass on the seed work nodes.
- Author seed: restart mass on the author node. Its works are one hop away (result type a); other authors are reached through shared subjects, series, or kNN edges (result type b).
- Restart probability α controls locality: high α keeps results near the seeds, low α lets them wander more (more lateral).
- Edge-type weights are the "what kind of similar" knob in graph mode.

Graph mode and embedding mode are complementary. Embeddings capture "reads like this"; the graph captures structure (same author, same series, same shelf) and makes multi-hop "lateral" jumps possible.

## 8. Query modes and semantics

```
recs query
  --seed "The Hunt for Red October" --seed "Tinker Tailor Soldier Spy" ...   # 1..n seed works
  --author "John le Carré"                                                     # or an author seed
  --not "The Da Vinci Code"                                                    # optional negative seeds
  --view content|genre|author|graph|blend
  --available-now | --max-wait 14
  --exclude-read (default on)  --exclude-authors-of-seeds (off)
  --diversity 0.0..1.0
  --limit 20
```

### Combining multiple seeds (embedding views)

- **Centroid:** average the seed vectors and query once. Simple; fine when the seeds are coherent (five spy thrillers).
- **Per-seed kNN + fusion (default):** query each seed separately, then merge with reciprocal-rank fusion or a mean of the top-m similarities. Robust when the seeds are mixed (a centroid of Clancy and Dostoevsky lands somewhere useless).
- **Negative seeds (Rocchio):** `q = mean(pos) − β·mean(neg)`. This is how to say "like these, but not that."

### Author query output

Two sections:

1. **More by this author** in the catalog that I haven't finished, sorted by availability.
2. **Similar authors:** nearest author vectors, plus PPR author-node scores, each shown with their top 2–3 borrowable works.

### Blending views (score calibration)

Raw cosine similarities aren't comparable across views: each view has its own distribution, and plot and excerpt vectors have different spreads from blurb vectors. Before blending:

1. **Center each view's embeddings** by subtracting that view's corpus mean (as with the `excerpt` view in §3.5).
2. **Rank-normalize each view's scores** over the candidate pool (percentile, 0–1). Z-scores are an alternative; ranks are more robust to the long tails.
3. Blend with weights, **renormalized over the views a candidate actually has**. A book with no Wikipedia plot shouldn't be penalized for the missing view, and a book that has every view shouldn't gain from having them.
4. Weights default to `content` 1.0, `genre` 0.5, `plot` 0.5, `excerpt` 0 (off). Tune them with the leave-one-out evaluation in §11.

### Classics mode

```
recs query --mode classics --seed "The Brothers Karamazov" --seed "Ethics" ...
```

- Restricts the search universe to catalog works that match a Gutenberg or Wikipedia text.
- Turns on the `excerpt` and `plot` views at real weights.
- Intended for exploring literature: finding the classics the library has as audiobooks that resemble the ones I already know.

### Backlog mode (quick win)

```
recs backlog --available-now
```

Books I **own but haven't finished**, or that are on my **to-read list**, that are available as audiobooks from the library. No model needed, just a join. Sort by availability, then by signal tier.

### Seed weights

In multi-seed queries, weight each seed by its §10 tier when fusing scores. That way the books I bought after finishing them pull harder than an unowned book I finished. Turn it off with `--unweighted`.

### Diversity

Maximal marginal relevance (MMR) over the candidate list: `λ·sim(query) − (1−λ)·max sim(already picked)`.
`--diversity 0` gives the tightest genre match (the "just find me spy thrillers" case); higher values push results sideways.

## 9. Ranking pipeline

```
seeds ─► retrieve (view-specific, top ~500)
      ─► filter (in catalog, audiobook, not finished, language, optional max wait)
      ─► rerank (optional, §10)
      ─► diversify (MMR)
      ─► attach live availability for the final ~50
      ─► output
```

Final display per result: title, author, narrator, duration, availability / est. wait, score, **why** (the closest seed and its similarity; shared subjects; "same series as X"), and a Libby link.

The "why" line matters: it's the fastest way to see whether a view is behaving.

## 10. Labels and optional reranker

### Label derivation

| Case | Label | Confidence |
|---|---|---|
| In ReadingList finished | 1 | high |
| Libby borrow, returned early, not finished | 0 | high |
| Libby borrow, loan expired, never re-borrowed, not finished | 0 | low (could be time constraints) |
| Libby borrow, renewed or re-borrowed, not finished | drop, or 1 with low confidence | — |
| Hold placed, never borrowed | no label | — |

**Provenance flag:** `to_read_list` if the work was added to ReadingList at least 30 days before the (reliable) Libby borrow date. `search` if it was never on the list, or was added after the borrow. `unknown` if it was added within the tolerance window: I may have found it in search and then added it on the spot. Exclude `unknown` from the provenance comparison.

### Signal tiers (ownership × finishing × sequence)

Ownership and finishing measure different things. Ownership is **interest before reading** (I chose to pay for it). Finishing is **the outcome** (it held my attention). The order of the two events carries information too.

| Tier | Condition | Reading | Default seed weight |
|---|---|---|---|
| T1 | Finished, then **bought afterward**: acquired ≥30 days after the finish date, with neither date flagged `user_batch` or `missing` | Liked it enough to want a copy | 1.5 |
| T2 | Owned and finished: the default whenever the order is unknown, which will be most cases | Interest confirmed by the outcome | 1.25 |
| T3 | Finished, not owned (mostly Libby/audio) | Outcome only | 1.0 |
| T4 | Owned, not finished | Interest only; **not** a negative. Mostly an unread backlog. | 0.5 (interest profile) / backlog candidate |
| T5 | On the to-read list, not owned | Interest only | 0.5 |
| N | Libby borrow returned early, not finished | Tried it and quit | negative seed / label 0 |

Caveats:

- **Owned-but-unfinished is not abandonment.** Without a "started" date (LibraryThing `datestarted`, if populated), an unread shelf book is unlabeled. Never label it 0.
- **Ownership is confounded with format.** I mostly buy print and mostly borrow audio, so "owned" partly means "print." That's fine for seed weighting. As a **reranker feature** it would leak format information, so keep ownership out of the reranker and use it only for seed weights and the interest profile.
- **Tier weights are guesses.** Calibrate them in §11: check whether T1/T2 seeds give better leave-one-out retrieval than T3 seeds.

### First analysis (before any modeling)

- Count labels by provenance and audio/print.
- Finish rate for list-sourced vs. search-sourced borrows (tests the "list ≈ always good, search ≈ coin flip" hypothesis).

### Reranker (phase 4, only if it earns its place)

- Training rows: **Libby audiobook borrows only** (the rows with both outcomes). Print-only finishes have no negatives; use them as seeds and as "similarity to finished set" features, not as training rows.
- Features: max/mean similarity to finished works, similarity to abandoned works, same author as something finished, series position, duration, narrator previously finished, publication decade, subject IDF overlap.
- Model: L2-regularized logistic regression. Small data; keep it interpretable.
- Must beat both baselines (§11) or it doesn't ship.

## 11. Evaluation

1. **Leave-one-out retrieval:** for each finished work that's in the catalog, hide it, use its 3–5 nearest *other* finished works as seeds, and record its rank among results. Report hit@10, hit@50, and median rank, per view.
2. **Finish/abandon discrimination:** AUC of each view's "similarity to finished set" score on Libby borrows (leave-one-out).
3. **Baselines:** (a) "same author as something I finished"; (b) random catalog books in the same top-level subject.
4. **Qualitative query suite:** a saved list of 10–15 queries with known expectations (a spy thriller set, a Russian novel set, a single author). Rerun after every change and diff the outputs.

## 12. Interface

- **Phase 1–3:** CLI only; output a table plus a markdown file per query.
- **Later:** a small server-rendered htmx page served on the home server over Tailscale. Pick seeds with search-as-you-type over my finished list, toggle the view, use sliders for diversity and max wait.

## 13. Stack

- Python, managed with `uv`; `ruff`.
- `httpx` (crawler), `sqlite3` (stdlib), `polars` (ETL), `numpy` / `scipy.sparse` (vectors, PPR), a local embedding model, `rapidfuzz` (entity resolution).
- No vector database or graph database: the scale doesn't call for either.
- Nightly job on the home server: availability refresh, plus a weekly catalog diff.

```
libby_recs/
  crawl/        thunder_client.py, crawl_catalog.py, refresh_availability.py
  ingest/       readinglist.py, libby_timeline.py, enrich_openlibrary.py
  resolve/      normalize.py, match.py, match_overrides.csv
  features/     embed.py, subjects.py, author_vectors.py, llm_tags.py
  graph/        build_graph.py, ppr.py
  query/        retrieve.py, fuse.py, mmr.py, explain.py, cli.py
  eval/         loo_retrieval.py, label_auc.py, query_suite.yaml
  data/         libby_recs.sqlite, cache/
```

## 14. Phases

| Phase | Deliverable | Done when |
|---|---|---|
| 0 | Ingest ReadingList + Libby timeline + LibraryThing, entity-resolve, derive labels and tiers | Labeled table, provenance finish rates, tier counts |
| 0.5 | `recs backlog` (owned/to-read ∩ available audiobooks) | First useful output; requires only a catalog lookup per backlog title |
| 1 | Catalog crawl + availability refresh into SQLite | Full audiobook collection stored, with a count that matches the Libby UI roughly |
| 2 | `content` embeddings + kNN query CLI (centroid and fusion) | The spy-thriller query returns spy thrillers |
| 3 | `genre` and `author` views, author query mode, graph + PPR, MMR | Author query returns both sections sensibly |
| 4 | Evaluation harness, then the reranker if warranted | LOO numbers per view; reranker beats baselines or is dropped |
| 5 | LLM tone tags, htmx UI, nightly job | Usable from my phone |
| 6 *(optional)* | Wikipedia `plot` view; Gutenberg `excerpt` view + classics mode | Classics-mode queries beat `content`-only on a hand-made literature query set |

## 15. Open questions

- Collection size, and whether Thunder pages cleanly through the whole thing.
- Does the Libby timeline carry ISBN or OverDrive id (much easier matching), or only title/author?
- Coverage check: what fraction of seeds get a non-thin blurb from Google Books or Open Library? (If it's low, the normalization step becomes more important.)
- Do raw cleaned blurbs or LLM-normalized summaries give better leave-one-out numbers?
- Which embedding model gives the best leave-one-out numbers on this data? Try 2–3 small local models.
- How noisy and coarse are OverDrive subjects vs. BISAC? Is Open Library enrichment worth doing for catalog works too, not just seeds?
- Should tone tags be retrieval dimensions or only filters ("pace: fast")?
- How many owned-and-finished books survive batch detection with a usable order? If it's only a handful, drop T1 and fold it into T2.
- Is a ReadingList finish date even good to the month? Check a few against Libby return dates for audiobooks, which are reliable.
- Does seed weighting by tier actually improve leave-one-out results, or are the tiers just intuition?
- Multiple library cards later? (The schema already allows it via `edition`/`availability`; add a `library_key` column.)

## 16. Risks

- **Thunder is undocumented** and could change or start rate-limiting. Keep the client isolated in one module; cache aggressively.
- **User-entered dates are fuzzy** (lazy logging, catch-up batches). Mitigation: §2 date rules — tolerance windows, batch detection, nothing that depends on a date being right.
- **Entity resolution errors** silently corrupt labels. The fuzzy-match review file and overrides CSV are the mitigation.
- **Descriptions are marketing copy**: two very different books can share "a pulse-pounding thriller" blurbs. Mitigations: blurb cleaning (§3.5), the `genre`/graph views and tone tags as cross-checks.
- **Text-source mismatch** inflates or deflates similarity. Mitigation: the symmetry rules in §3.5.
- **Copyright:** blurbs and summaries are copyrighted but used here for private embeddings only. Don't republish them if the UI is ever exposed beyond my own tailnet; link out instead. No full texts except public-domain Gutenberg works.
- **Small labeled set (~100 audio borrows with both outcomes):** keep the reranker simple and treat it as optional.
