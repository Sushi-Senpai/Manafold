# Arrow: import-export

Decklist parsing and emission — plain-text, MTGA, Moxfield, Archidekt — plus the
`imports` audit table, parse-then-apply endpoints, and unresolved-name
reporting.

## Status

**MAPPED** — LLD + specs authored 2026-09-02; parsers, emitters, and the
parse-then-apply endpoints implemented at M2. `PORT-008` (live single-printing
fallback) waits on `card-data`'s `CARD-009`.

## References

### HLD
- docs/high-level-design.md (Goals — import/export parity; Success Metrics — falsification signal on a silently dropped card)

### LLD
- docs/intent/import-export/import-export-design.md

### EARS
- docs/intent/import-export/import-export-specs.md (PORT-001..008, PORT-020..022)

### Tests
- backend/internal/deckio/parse_test.go — PORT-002 (plain-text sections, MTGA set codes + blank-line sideboard, Moxfield foil + `SB:`), PORT-003 (Archidekt `[Category]` / `[Category{top}]`), quantity forms + rejects, split-face verbatim
- backend/internal/deckio/emit_test.go — PORT-007 (plain-text has no printing refs, MTGA carries `(SET) #`, emit→parse round trip)
- backend/internal/api/imports_test.go — PORT-001, PORT-004 (unresolved reported verbatim), PORT-005 (split face folds to whole card), PORT-006 / DECK-060 (apply writes in one transaction, board + category preserved), DECK-009 (non-owner 404), PORT-007 (export end to end)

### Code
- backend/internal/deckio/ (`Parse`, `EmitPlaintext`, `EmitMTGA` — pure)
- backend/internal/api/imports.go (`RegisterImportRoutes`, `parseImport` / `applyImport` / `exportDeck`, `resolveLines`)
- backend/internal/db/migrations/000004_create_imports.up.sql / .down.sql
- backend/internal/db/queries/imports.sql (`CreateImport`, `GetImportForOwner`, `MarkImportApplied`)
- backend/internal/db/queries/cards.sql (`ResolveCardByName` — exact or DFC face)
- frontend/src/app/(app)/decks/[id]/page.tsx (Import / export panel)
- frontend/src/lib/api.ts (`parseImport` / `applyImport` / `exportDeck`)

## Architecture

**Purpose:** get a deck in from and out to the community text formats, losslessly
or loudly lossy.

**Key components:**
1. `imports` audit table — raw text + parsed result (`deckio.ParseResult`) +
   unresolved lines + `applied_at`.
2. `internal/deckio` — one tolerant line grammar for all four formats (formats
   differ only in header handling); plain-text + MTGA emitters. Pure: no
   resolution, no DB.
3. Name resolution against `cards` (`internal/api`) — exact case-insensitive, or
   one face of a split/DFC card → whole card (`PORT-005`). A name that matches
   nothing is reported and stored, never dropped (`PORT-004`); a line the
   grammar cannot read is reported separately as *rejected*.
4. Parse-then-apply endpoints — `POST /import` (parse + audit, no write),
   `POST /import/{importId}/apply` (one transaction, `DECK-060`, marks applied),
   `GET /export?format=plaintext|mtga`. Ownership scoped in the queries → `404`.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Import / export | PORT-001..007 | 7 | 0 | 0 |
| Single-printing fallback | PORT-008 | 0 | 0 | 1 (waits on CARD-009) |
| Legacy formats / paste-to-new-deck / printing export | PORT-020..022 | 0 | 3 | 0 |

**Summary:** 7 of 11 implemented; 1 gap (`PORT-008`, blocked on `CARD-009`); 3
deferred.

## Key Findings

1. Import is parse-then-apply with an audit row between — the user sees what
   resolved (and what did not) before committing 100 cards.
2. Unresolved names and unreadable lines are two lists with two different fixes;
   neither is ever silently dropped — a named HLD falsification signal.
3. `internal/deckio` is pure (text ⇄ structs only); resolution and the DB write
   live in `internal/api`, keeping the grammar exhaustively table-testable and
   reusable by `ai-assist`'s emitters.
4. Archidekt `[Category]` tags survive import into `deck_cards.category`, where
   `internal/deckstats` then rolls them up (`DECK-052`).

## Work Required

### Must Fix
(none)

### Should Fix
1. `PORT-008` — pin an imported `(SET) collector#` to an exact printing via the
   `CARD-009` fallback, once that and printing-selection UI land.

### Nice to Have
1. Auto-assign the commander from the first `Commander` line on import (see LLD
   Open Questions gap 4).
2. Trigram (`pg_trgm`) fuzzy name matching for typo tolerance (gap 5).
3. `.dec` / `.cod` (`PORT-020`); paste-to-new-deck (`PORT-021`).
