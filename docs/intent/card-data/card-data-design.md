---
parent: high-level-design
prefix: CARD
---

# Card Data

## Context and Design Philosophy

Manafold owns its card layer. Every search, autocomplete, and card-detail request
is served from Manafold's own Postgres, populated by a job that ingests
Scryfall's bulk-data exports. Scryfall's card endpoints are never called on a
client request path — only by the sync job and by an explicit single-printing
fallback on card add (that fallback is designed here, implemented no earlier than
M2). This is what makes deck validation a SQL join rather than a rate-limited API
call, and it is a direct expression of the *own the card data* tenet.

The model separates the **Oracle card** (rules-relevant, one row per Scryfall
`oracle_id` — the unit legality is checked against) from the **printing**
(art/set/price, one row per Scryfall `id` — the unit display uses).

## Schema

Migration `create_card_data` creates `cards`, `card_prints`, `card_rulings`,
`sync_runs`, and `banlist_overrides`. UUID PKs via `gen_random_uuid()`, the
shared `set_updated_at()` trigger (defined here since this is the first
migration), `jsonb` for `legalities` / `prices` / `image_uris` / `card_faces`,
`ON DELETE CASCADE` on child FKs.

### `cards` (Oracle layer)

| column | type | notes |
|---|---|---|
| `id` | uuid pk | Manafold's own id |
| `scryfall_oracle_id` | uuid unique not null | join key to Scryfall Oracle |
| `name` | text not null | English name; the singleton key |
| `mana_cost` | text null | null for split/DFC — see `card_faces` |
| `mana_value` | numeric not null | Scryfall `cmc` |
| `type_line` | text not null | |
| `oracle_text` | text not null default `''` | |
| `colors` | text[] not null default `'{}'` | WUBRG subset |
| `color_identity` | text[] not null default `'{}'` | **from Scryfall — never recomputed** |
| `produced_mana` | text[] null | |
| `keywords` | text[] not null default `'{}'` | |
| `power`, `toughness`, `loyalty` | text null | text, because `*` / `1+*` exist |
| `legalities` | jsonb not null default `'{}'` | full per-format map; `legalities->>'commander'` is the validation input |
| `is_game_changer` | bool not null default false | Scryfall `game_changer` |
| `is_reserved` | bool not null default false | |
| `layout` | text not null | `normal` / `modal_dfc` / `transform` / `split` / `adventure` / … |
| `card_faces` | jsonb null | per-face name/cost/type/text/image for multi-face |
| `singleton_limit` | int null | **null = normal singleton; 0 = unlimited; N = capped.** Derived at sync from Oracle text |
| `can_be_commander` | bool not null default false | **derived at sync**: legendary creature, or text grants it |
| `commander_color_identity` | text[] null | usually `= color_identity`; a few "commander" partners differ |
| `edhrec_rank` | int null | |
| `oracle_search` | tsvector | generated / maintained from `name || ' ' || oracle_text || ' ' || type_line`; GIN-indexed |
| `created_at`, `updated_at` | timestamptz not null default `now()` | |

Indexes: unique on `scryfall_oracle_id`; btree on `lower(name)`, `mana_value`,
`edhrec_rank`; GIN on `color_identity`, `oracle_search`.

### `card_prints` (printing layer)

| column | type | notes |
|---|---|---|
| `id` | uuid pk | |
| `scryfall_id` | uuid unique not null | Scryfall printing id |
| `card_id` | uuid fk → cards on delete cascade | |
| `set_code`, `set_name` | text not null | |
| `collector_number` | text not null | |
| `rarity` | text not null | |
| `released_at` | date null | |
| `finishes` | text[] not null default `'{}'` | `nonfoil` / `foil` / `etched` |
| `image_uris` | jsonb null | `small` / `normal` / `large` / `png` / `art_crop` |
| `prices` | jsonb null | `usd` / `usd_foil` / `eur` / `tix` |
| `is_promo`, `is_reprint`, `is_digital` | bool not null default false | |
| `created_at`, `updated_at` | timestamptz | |

Index: btree on `(card_id, released_at desc nulls last)` — "newest printing" is
the default selection.

### `card_rulings`

`id`, `card_id` fk → cards on delete cascade, `scryfall_oracle_id`, `comment`
text, `published_at` date. Unique on `(card_id, published_at, md5(comment))` so
re-sync is idempotent.

### `sync_runs`

`id`, `bulk_type` (`oracle_cards` / `default_cards` / `rulings`),
`scryfall_updated_at` timestamptz, `rows_upserted` int, `status`
(`running` / `succeeded` / `failed`), `started_at`, `finished_at` null,
`error` text null.

### `banlist_overrides`

`id`, `card_name` text unique, `banned` bool not null, `note` text, `created_at`.
The escape hatch for the gap between a Commander Format Panel announcement and
Scryfall's next data refresh. `deck-building`'s validator consults this table
after `legalities->>'commander'`.

## Sync Job (`internal/cardsync`)

`cardsync.Run(ctx, pool, opts)` is the ingestion entry point, called by
`cmd/cardsync/main.go` (and, in tests, directly with a fixture).

`opts.OracleCardsPath` / `opts.DefaultCardsPath` replace the download with a
local file for that pass. `cmd/cardsync` sets them from `CARDSYNC_SEED_PATH`
(one file feeds both passes) or the per-pass `CARDSYNC_ORACLE_PATH` /
`CARDSYNC_DEFAULT_PATH`. `backend/seed/cards.json` is the checked-in dev / CI
seed — a JSON array of real Scryfall card objects (commanders, a colour / type /
mana-value spread, two modal double-faced cards, one deliberately imageless
printing) so a local run and CI have searchable, image-bearing card data
without a live Scryfall fetch (`CARD-040`).

1. `GET https://api.scryfall.com/bulk-data` — the manifest. Read the
   `jsonl_download_uri` and `updated_at` for `oracle_cards` and `default_cards`
   (and `rulings` when `opts.IncludeRulings`). A manifest entry missing
   `jsonl_download_uri` fails the run.
2. **Download, then ingest — never both at once.** For each export, `GET` the
   `jsonl_download_uri` (a `.jsonl.gz` on `data.scryfall.io`, served as
   `application/gzip` with no `Content-Encoding`) and stream the response body
   straight to a temporary file, then close the HTTP connection. Only then is a
   `sync_runs` row inserted with `status = 'running'` and the ingest begun,
   reading the export back from local disk: `compress/gzip` inflates the file and
   the inflated stream is decoded as newline-delimited JSON, one card object per
   line. The download is not gated on database-write latency, so a
   ~78 MB Default Cards export leaves the connection in seconds rather than
   trickling for the length of the ingest; the temp file (compressed, ~25 MB
   Oracle + ~78 MB Default at peak) is removed on both the success and the
   failure path. A local fixture supplied through `opts.OracleCardsPath` /
   `opts.DefaultCardsPath` is read directly — no download, no temp file — and is
   uninflated (the gzip layer is transport-only); it may be either
   newline-delimited JSON or a single top-level JSON array of card objects — the
   decoder detects the leading `[` and unwraps it. `backend/seed/cards.json` is
   the array form, the shape Scryfall's card APIs return. The decoder consumes
   consecutive JSON values across newlines, so no per-line length limit applies
   and a multi-hundred-MB export is never buffered in memory whole.
3. **Oracle Cards → `cards`**: upsert by `scryfall_oracle_id`, one row per
   object. Derive `singleton_limit`, `can_be_commander`,
   `commander_color_identity` (see below). `color_identity` is copied straight
   from the object's `color_identity` array.
4. **Default Cards → `card_prints`**: bulk upsert, not one round-trip per
   printing. Every decoded object is `COPY`d into a session `TEMP` staging table
   (`ON COMMIT DROP`), then a single `INSERT INTO card_prints SELECT … FROM
   staging JOIN cards ON cards.scryfall_oracle_id = staging.oracle_id ON CONFLICT
   (scryfall_id) DO UPDATE …` merges the batch. The `JOIN` resolves `card_id` and
   silently drops any printing whose `oracle_id` is not in `cards` (the Oracle
   file is the source of truth for which cards exist); those orphans, plus any
   object with an unparseable `oracle_id` (dropped before staging), are counted
   as skipped. A duplicate `scryfall_id` inside one export collapses via
   `DISTINCT ON` rather than failing the merge.
5. On success, update the `sync_runs` row: `status = 'succeeded'`,
   `rows_upserted`, `finished_at`. On any error, `status = 'failed'`, `error`,
   `finished_at`, and return the error.

### HTTP etiquette (non-negotiable)

Every request sends `User-Agent: Manafold/1.0
(github.com/Sushi-Senpai/Manafold)` and `Accept:
application/json;q=0.9,*/*;q=0.8`. Sustained traffic stays under 10 req/s with
50–100 ms between calls; the bulk downloads are two large streamed GETs spooled
to a temp file, not a crawl. An HTTP `429` triggers a 30 s back-off then one
retry. The non-body phases are each bounded so a stalled connection cannot hang
a `context.Background()` run: the manifest fetch **and its JSON decode** run
under a short `context.WithTimeout` (a few-KB document fetched once), and each
bulk GET's wait for response headers is bounded by a **response-header
timeout**. Each bulk download is bounded by an **idle deadline** — the run
fails if the connection delivers no bytes for a bounded interval — and by a
**generous total deadline** (~30 min) over the whole file download, a backstop
that fails a CDN trickling just enough bytes to keep the idle timer alive. None
of these is a single whole-request timeout, which would span the subsequent
ingest and abort a healthy run. The `/cards/collection` batch endpoint (used only by
the M2 single-printing fallback) is held under ~2 req/s.

### Derived fields

- **`singleton_limit`** — parsed from Oracle text, case-insensitively:
  - "A deck can have any number of cards named {name}" → `0` (unlimited).
  - "A deck can have up to N cards named {name}" / "up to seven" (spelled
    numbers) → `N`.
  - Otherwise → `NULL` (normal singleton). Basic lands are *not* special-cased
    here — `deck-building`'s validator exempts them by type line.
- **`can_be_commander`** — true when the card's `type_line` contains both
  `Legendary` and `Creature`, OR the Oracle text contains "can be your
  commander". For a multi-face card, either face qualifying is enough.
- **`commander_color_identity`** — equals `color_identity` unless the Oracle text
  names a distinct commander identity (rare "partner" wording); defaults to a
  copy of `color_identity`.

## HTTP API

| Endpoint | Method | Query | Response |
|---|---|---|---|
| `/api/cards/search` | GET | `q` (query string), `page` | `{ cards: CardSummary[], total, has_more }` |
| `/api/cards/autocomplete` | GET | `q` (prefix) | `{ names: string[] }` (≤ 20) |

`GET /api/cards/autocomplete` returns up to 20 card names whose `lower(name)`
starts with `lower(q)` (trigram/prefix), ordered by `edhrec_rank` ascending
(nulls last). Empty `q` → empty list.

`GET /api/cards/search` parses a **subset of Scryfall query syntax** over the
mirror. M1 supports:

- bare words → full-text match against `oracle_search` (AND of terms) and a
  `name ILIKE '%term%'` fallback.
- `id:<letters>` / `id<=<letters>` / `id=<letters>` → colour-identity predicate.
  `id:wug` means "colour identity is a subset of {W,U,G}"; `id=wug` means exact;
  `id>=wug` means superset. Letters are WUBRG (+ `c` for colourless).
- `t:<word>` → `type_line ILIKE '%word%'`.
- `cmc<=N` / `cmc>=N` / `cmc=N` / `cmc:N` → `mana_value` comparison.
- `o:"<phrase>"` / `o:word` → Oracle-text phrase match.
- `is:commander` → `can_be_commander = true`.

Results are `unique = cards` (Oracle rows). Each `CardSummary` carries the
card's Oracle fields plus its newest printing's `image_uris` and `prices`
(the M1 default; no printing-selection UI). Ordering: `edhrec_rank` asc nulls
last, then `name`. Page size 60.

The parser is a small hand-written tokenizer in `internal/cardsearch`
(`Parse(string) (Query, error)`), separate from the handler and unit-tested,
because Scryfall's syntax grows and the parse step is where a
plausible-but-wrong query silently returns the wrong cards.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Oracle vs printing split | Two tables: `cards` (one per `oracle_id`) + `card_prints` (one per printing `id`) | One denormalised card table | Legality is an Oracle-level property checked once per distinct card; a card like Counterspell has 70+ printings. Decks reference the Oracle row for rules and a printing for display. |
| `color_identity` provenance | Store Scryfall's array verbatim; never compute | Derive from `mana_cost` + `oracle_text` | Scryfall already handles hybrid/Phyrexian pips, reminder-text exclusion, colour indicators, and the union across DFC faces. Recomputing is pure downside. |
| `singleton_limit` provenance | Derive at sync from Oracle text; `NULL`/`0`/`N` tri-state | A hard-coded list of exception cards (Rat Colony, Dragon's Approach, …); a boolean | New cards with the "any number" clause ship every set; a list goes stale. The tri-state distinguishes "unlimited" from "capped at N" (Seven Dwarves, Nazgûl). |
| Banlist source | Scryfall `legalities.commander` + a `banlist_overrides` table | A hand-curated banlist | Scryfall's field already merges functional bans, ante cards, *Conspiracy* cards, offensive-content bans, and `restricted`/`not_legal`, and tracks Panel action. The override table covers only the announcement-to-refresh gap. |
| Query parsing | Hand-written tokenizer in `internal/cardsearch`, unit-tested, separate from the handler | Inline `if strings.Contains` in the handler; a full Scryfall-syntax library | The subset is small and grows predictably; keeping it isolated and tested is where a wrong-cards regression gets caught. A full library does not exist for Go and the full grammar is out of v1 scope. |
| Sync invocation | `cmd/cardsync` binary as a Render cron | In-process goroutine ticker in `cmd/api` | A separate process has no HTTP surface and does not couple sync to API uptime or duplicate work across API instances. |
| Bulk transport decode | Follow the manifest's `jsonl_download_uri`; inflate the `.jsonl.gz` body with `compress/gzip`; decode newline-delimited JSON one object at a time | Rely on `net/http` transparent decompression; buffer the whole file then `json.Unmarshal` an array | Scryfall serves the export as `application/gzip` with no `Content-Encoding`, so the transport never inflates it; the payload is JSONL, not a JSON array. Streaming the inflated stream keeps a multi-hundred-MB export off the heap. |
| Download vs. ingest coupling | Spool each export to a temp file first (closing the connection), then ingest from local disk; remove the temp file on every exit path | Ingest straight off the live HTTP body, one object read interleaved with its DB write | Interleaving made every body read wait on Postgres write latency, so the connection stayed open for the whole ingest and was torn down mid-stream (a `PROTOCOL_ERROR` surfaced from the CDN or an `http.Client` deadline), leaving ~87% of cards with no printing row. A spooled download is bounded and quick; the disk cost is the compressed export size, released immediately after. |
| Printing upsert shape | `COPY` all printings into a session `TEMP` table, then one `INSERT … SELECT … JOIN cards … ON CONFLICT DO UPDATE` | One `SELECT` for `card_id` plus one `INSERT … ON CONFLICT` per printing (~118 k × 2 round-trips) | The per-row path is the reason a real sync cannot finish inside the cron wall-clock and why the connection is held open so long. `COPY` + a set-based merge is two statements for the whole export, resolves `card_id` in SQL, and yields the orphan-skip count from a `LEFT JOIN`. The `TEMP` table is session-scoped so concurrent syncs never collide. |
| Bulk download deadline | A short `context.WithTimeout` around the manifest fetch+decode, `Transport.ResponseHeaderTimeout` for each bulk GET's response-header wait, and — for each bulk body — a per-download idle/read deadline (fail if no bytes arrive for a bounded interval) plus a generous total `context.WithTimeout` (~30 min) over the whole file download | A single `http.Client{Timeout}` covering the whole request | A whole-request timeout has to be set long enough for the full ingest, so it cannot also catch a genuinely stalled download promptly; once download and ingest are decoupled it would only ever fire spuriously on a healthy long ingest. The manifest timeout and `ResponseHeaderTimeout` fail a connection that completes TLS but never answers (or stalls mid-manifest) — otherwise unbounded under `cmd/cardsync`'s `context.Background()` — while the idle deadline fails a dead connection mid-body and the total deadline backstops a CDN trickling just enough bytes to keep the idle timer alive; none touches the ingest. |
| Full-text index | Postgres `tsvector` column (`oracle_search`) + GIN | `pg_trgm` only; an external search engine | `tsvector` handles the Oracle-text predicates natively at sub-10 ms; `pg_trgm` additionally backs the name prefix/`ILIKE` paths. No external dependency. |

## Open Questions & Future Decisions

### Deferred
1. **Single-printing fallback on card add** — when a user pastes a collector
   number not yet mirrored, one `GET /cards/collection` call (≤ 2 req/s) fetches
   and upserts just that printing. Designed here; implemented at M2 alongside
   import/export, which is where unmirrored printings first show up in bulk.
2. **`oracle_tags` ingestion** — Scryfall's community functional-tag bulk file,
   to seed `deck-building`'s auto-categorizer. A third `bulk_type` for
   `cardsync`; deferred to M2/M5 with the real categorizer.
3. **In-process ticker fallback** — if the Render cron is unavailable on the
   free tier, a guarded goroutine ticker in `cmd/api` is an acceptable
   single-instance stopgap; revisit before any multi-instance deploy.
4. **`All Cards` (every language)** — v1 mirrors English-only Default Cards.
   Non-English printing support is a later, larger ingestion.
5. **Batch the Oracle Cards ingest too** — `ingestOracle` still upserts one
   `cards` row per object (~40 k round trips). It survives because the download
   is now spooled first, so nothing gates the HTTP connection, and 40 k
   single-table upserts finish well inside the cron wall-clock. If the Oracle
   export grows or the derived-field logic gets heavier, give it the same
   `COPY` + set-based merge treatment `card_prints` uses. (The `card_prints`
   batching that this item originally called for has landed.)

### Gaps
6. **Prices go stale between daily syncs** — acceptable; prices are advisory and
   the daily cadence matches Scryfall's own regeneration.
7. **A `429` during a bulk stream** — retried once after a 30 s back-off; a
   second failure fails the `sync_runs` row and the job exits non-zero for the
   cron to surface. No partial-resume of a half-streamed file in v1.

## References

- Code: `backend/internal/cardsync/`, `backend/internal/cardsearch/`,
  `backend/internal/api/cards.go`, `backend/cmd/cardsync/main.go`
  (`seedOptions`), `backend/internal/db/migrations/000001_create_card_data.up.sql`,
  `backend/internal/db/queries/cards.sql`
- Seed: `backend/seed/cards.json` (dev / CI card seed, `CARD-040`)
- Test fixtures: `backend/internal/cardsync/testdata/`; seed test
  `backend/internal/cardsync/seed_test.go`
- Cross-segment: `deck-building`'s validator reads `cards.color_identity`,
  `cards.singleton_limit`, `cards.can_be_commander`, `cards.legalities`, and
  `banlist_overrides` (`DECK-004` / `DECK-006` / `DECK-008`). `import-export`
  (`PORT`) and `ai-assist` (`AI`) resolve card names against `cards`.
- Scryfall: <https://scryfall.com/docs/api/bulk-data>,
  <https://scryfall.com/docs/api/rate-limits>, <https://scryfall.com/docs/terms>.
