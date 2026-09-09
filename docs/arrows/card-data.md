# Arrow: card-data

The Scryfall mirror: `cards` / `card_prints` / `card_rulings` / `sync_runs` /
`banlist_overrides`, the `internal/cardsync` ingestion job, the
`internal/cardsearch` query parser, and `GET /api/cards/search` + `/autocomplete`.

## Status

**MAPPED** — greenfield, authored with the M1 slice (2026-09-02); bulk-download
path corrected to `jsonl_download_uri` + gzip-inflated JSONL (CARD-012, 2026-09-06);
sync hardened to download-then-ingest with a temp-file spool and a `COPY`-based
batched `card_prints` upsert so the full Default Cards export lands (CARD-013 /
CARD-014 / CARD-015, 2026-09-09).

## References

### HLD
- docs/high-level-design.md (Approach — Own the card layer; Key Design Decisions — mirror / color_identity / banlist)

### LLD
- docs/intent/card-data/card-data-design.md

### EARS
- docs/intent/card-data/card-data-specs.md (CARD-001..015, CARD-020..024, CARD-030, CARD-040)

### Tests
- backend/internal/cardsync/cardsync_test.go (`TestRun_IngestsFixture_DerivesFields`) — CARD-001, CARD-002 (verbatim color_identity), CARD-003, CARD-004, CARD-005, CARD-007, CARD-014 (the `COPY`-merged `card_prints` rows carry the same `set_code` / `finishes` / `image_uris` / `prices` the per-row path wrote, and the orphan printing has no row); reads the checked-in `.jsonl` fixtures
- backend/internal/cardsync/endtoend_test.go (`TestRun_EndToEnd_ScryfallShapedGzipManifest`) — CARD-001, CARD-012, CARD-013 (a full `Run` over a Scryfall-shaped gzip manifest leaves no temp file behind), CARD-020
- backend/internal/cardsync/seed_test.go (`TestRun_DevSeed`) — CARD-040 (backend/seed/cards.json ingests through both passes; real Scryfall art on most prints, at least one imageless, commander rules fields derived)
- backend/internal/cardsync/derive_test.go (`TestDeriveSingletonLimit`, `TestDeriveCanBeCommander`) — CARD-003, CARD-004
- backend/internal/cardsync/httpfetch_test.go (`TestFetcher_SendsEtiquetteHeadersAndRetriesOn429`, `TestFetcher_NonRetryableStatusIsAnError`) — CARD-006 (descriptive User-Agent + explicit Accept on every request; exactly one retry after the back-off on HTTP 429), CARD-007
- backend/internal/cardsync/httpfetch_test.go (`TestManifest_ResolvesJSONLDownloadURI`, `TestOpenBulkFile_InflatesGzippedJSONL`, `TestOpenBulkFile_NonGzipIsAnError`, `TestStreamJSONObjects_DecodesNewlineDelimited`) — CARD-001 (manifest → `jsonl_download_uri`, missing field fails), CARD-012 (gunzip the spooled `application/gzip` file, decode newline-delimited JSON incrementally, non-gzip file is an error), CARD-007
- backend/internal/cardsync/download_test.go (`TestBulkDownload_SpoolDecouplesIngestFromTheConnection`, `TestDownloadToTemp_IdleConnectionFailsAndCleansUp`, `TestDownloadToTemp_RoundTripsGzipBody`) — CARD-013 (spool to a temp file, connection closed before any DB write, temp file removed on success and on error), CARD-015 (a slow downstream consumer no longer resets the download; a stalled connection fails on the idle deadline). Runs without Postgres — reproduces the slow-consumer failure mode and proves the spool fixes it.
- backend/internal/cardsearch/cardsearch_test.go — CARD-022, CARD-023
- backend/internal/api/cards_test.go — CARD-020, CARD-021, CARD-023
- CARD-008 / CARD-024 (no Scryfall call on a client request path) are a negative
  invariant: the `/api/cards/*` handlers depend only on the pool and the
  generated queries, and their tests pass with no network configured. No
  dedicated `@spec` test asserts the absence.

### Code
- backend/internal/cardsync/ (`Run`, bulk manifest fetch → `jsonl_download_uri`, `downloadToTemp` spool + `idleReader` deadline, `openBulkFile` gzip inflation, `streamJSONObjects` / `jsonObjectStream` incremental JSONL decode, `ingestOracle` per-row upsert, `copyUpsertPrints` `COPY` + set-based merge, derived fields)
- backend/internal/cardsearch/ (`Parse`, predicate → SQL via `Query.WhereSQL`)
- backend/internal/api/cards.go (`registerCardRoutes`, search + autocomplete handlers)
- backend/cmd/cardsync/main.go (`seedOptions` — CARDSYNC_SEED_PATH / CARDSYNC_ORACLE_PATH / CARDSYNC_DEFAULT_PATH, CARD-040)
- backend/seed/cards.json (checked-in dev / CI card seed — CARD-040)
- backend/internal/db/migrations/000001_create_card_data.up.sql / .down.sql
- backend/internal/db/queries/cards.sql

## Architecture

**Purpose:** own the card layer so deck validation is a SQL join, not an API call.

**Key components:**
1. Schema — Oracle (`cards`) vs printing (`card_prints`) split; `legalities` /
   `prices` / `image_uris` / `card_faces` as jsonb; `oracle_search` tsvector +
   GIN; `banlist_overrides` escape hatch.
2. `internal/cardsync` — manifest fetch (`jsonl_download_uri`), download-then-
   ingest: each export is spooled to a temp file (bounded by an idle deadline,
   removed on every exit) then read from disk, gzip-inflated, and decoded as
   incremental JSONL. Oracle Cards upsert one row at a time; Default Cards are
   `COPY`d to a `TEMP` staging table and merged into `card_prints` with a single
   set-based `INSERT … ON CONFLICT`. `sync_runs` audit, HTTP etiquette
   (descriptive UA + Accept, 429 back-off), derived `singleton_limit` /
   `can_be_commander` / `commander_color_identity`.
3. `internal/cardsearch` — hand-written tokenizer for the Scryfall-syntax subset
   (`id:` / `t:` / `cmc` / `o:` / `is:commander` + full text).
4. HTTP — `/api/cards/search` (paged) + `/api/cards/autocomplete` (≤ 20, by
   `edhrec_rank`), mirror-only.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Sync job | CARD-001..008, CARD-012..015 | 12 | 0 | 0 |
| Single-printing fallback | CARD-009 | 0 | 0 | 1 (M2+ — see below) |
| oracle_tags / all_cards | CARD-010, CARD-011 | 0 | 2 | 0 |
| Search & autocomplete | CARD-020..024 | 5 | 0 | 0 |
| Banlist overrides | CARD-030 | 1 | 0 | 0 |
| Dev / CI seed | CARD-040 | 1 | 0 | 0 |

**Summary:** 18 of 20 implemented; one gap: CARD-009, the single-printing
Scryfall fallback, deferred past M2's import/export (import resolves names
against the mirror; an unmirrored `(SET) collector#` currently falls through to
name resolution rather than triggering a live fetch). 2 deferred (CARD-010/011).

## Key Findings

1. `color_identity` is stored verbatim from Scryfall and never recomputed
   (CARD-002) — the DFC/hybrid/Phyrexian edge cases are Scryfall's problem.
2. `singleton_limit` is a `NULL` / `0` / `N` tri-state derived from Oracle text,
   not a hard-coded exception list (CARD-003).
3. Legality validates against `legalities->>'commander'` plus `banlist_overrides`
   — no hand-curated banlist (CARD-030).
4. The sync downloads each export to a temp file and closes the connection
   before touching Postgres (CARD-013); the per-printing DB round trip that held
   the connection open for the whole ingest — and lost ~87% of `card_prints` to
   a mid-stream `PROTOCOL_ERROR` — is now a `COPY` into a `TEMP` table plus one
   set-based merge (CARD-014).

## Work Required

### Must Fix
(none)

### Should Fix
1. CARD-009 single-printing fallback: still a gap after M2. M2 import resolves
   each line's name against the mirror and reports anything unmatched as
   unresolved (PORT-004); a set code + collector number that names a printing
   not in `card_prints` is not yet fetched live. Land the `/cards/collection`
   fetch (held under ~2 req/s) when printing selection UI arrives so an imported
   list can pin its exact printings.

### Nice to Have
1. `oracle_tags` ingestion (CARD-010) to seed the functional auto-categorizer.
