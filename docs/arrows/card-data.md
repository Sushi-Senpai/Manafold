# Arrow: card-data

The Scryfall mirror: `cards` / `card_prints` / `card_rulings` / `sync_runs` /
`banlist_overrides`, the `internal/cardsync` ingestion job, the
`internal/cardsearch` query parser, and `GET /api/cards/search` + `/autocomplete`.

## Status

**MAPPED** — greenfield, authored with the M1 slice (2026-09-02).

## References

### HLD
- docs/high-level-design.md (Approach — Own the card layer; Key Design Decisions — mirror / color_identity / banlist)

### LLD
- docs/intent/card-data/card-data-design.md

### EARS
- docs/intent/card-data/card-data-specs.md (CARD-001..011, CARD-020..024, CARD-030)

### Tests
- backend/internal/cardsync/cardsync_test.go (`TestRun_IngestsFixtureAndDerivesFields`) — CARD-001, CARD-002 (verbatim color_identity), CARD-003, CARD-004, CARD-005, CARD-007
- backend/internal/cardsync/httpfetch_test.go (`TestHTTPFetcher_SendsEtiquetteHeadersAndRetriesOn429`) — CARD-006
- backend/internal/cardsearch/cardsearch_test.go — CARD-022, CARD-023
- backend/internal/api/cards_test.go — CARD-020, CARD-021, CARD-023
- CARD-008 / CARD-024 (no Scryfall call on a client request path) are a negative
  invariant: the `/api/cards/*` handlers depend only on the pool and the
  generated queries, and their tests pass with no network configured. No
  dedicated `@spec` test asserts the absence.

### Code
- backend/internal/cardsync/ (`Run`, bulk manifest fetch, streaming JSONL upsert, derived fields)
- backend/internal/cardsearch/ (`ParseQuery`, predicate → SQL)
- backend/internal/api/cards.go (`registerCardRoutes`, search + autocomplete handlers)
- backend/cmd/cardsync/main.go
- backend/internal/db/migrations/000001_create_card_data.up.sql / .down.sql
- backend/internal/db/queries/cards.sql

## Architecture

**Purpose:** own the card layer so deck validation is a SQL join, not an API call.

**Key components:**
1. Schema — Oracle (`cards`) vs printing (`card_prints`) split; `legalities` /
   `prices` / `image_uris` / `card_faces` as jsonb; `oracle_search` tsvector +
   GIN; `banlist_overrides` escape hatch.
2. `internal/cardsync` — manifest fetch, streamed gzip JSONL upsert, `sync_runs`
   audit, HTTP etiquette (descriptive UA + Accept, 429 back-off), derived
   `singleton_limit` / `can_be_commander` / `commander_color_identity`.
3. `internal/cardsearch` — hand-written tokenizer for the Scryfall-syntax subset
   (`id:` / `t:` / `cmc` / `o:` / `is:commander` + full text).
4. HTTP — `/api/cards/search` (paged) + `/api/cards/autocomplete` (≤ 20, by
   `edhrec_rank`), mirror-only.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Sync job | CARD-001..008 | 7 | 0 | 0 (CARD-006 429-retry exercised via a stubbed transport) |
| Single-printing fallback | CARD-009 | 0 | 0 | 1 (M2) |
| oracle_tags / all_cards | CARD-010, CARD-011 | 0 | 2 | 0 |
| Search & autocomplete | CARD-020..024 | 5 | 0 | 0 |
| Banlist overrides | CARD-030 | 1 | 0 | 0 |

**Summary:** 13 of 16 implemented; 1 gap (CARD-009, M2); 2 deferred.

## Key Findings

1. `color_identity` is stored verbatim from Scryfall and never recomputed
   (CARD-002) — the DFC/hybrid/Phyrexian edge cases are Scryfall's problem.
2. `singleton_limit` is a `NULL` / `0` / `N` tri-state derived from Oracle text,
   not a hard-coded exception list (CARD-003).
3. Legality validates against `legalities->>'commander'` plus `banlist_overrides`
   — no hand-curated banlist (CARD-030).

## Work Required

### Must Fix
(none)

### Should Fix
1. CARD-009 single-printing fallback lands with import/export (M2), where
   unmirrored printings first appear in bulk.

### Nice to Have
1. `oracle_tags` ingestion (CARD-010) to seed the functional auto-categorizer.
