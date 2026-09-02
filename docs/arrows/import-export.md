# Arrow: import-export

Decklist parsing and emission — plain-text, MTGA, Moxfield, Archidekt — plus the
`imports` audit table and unresolved-name reporting. No code in M1.

## Status

**DRAFT** — LLD + specs authored 2026-09-02. Implementation begins at M2.

## References

### HLD
- docs/high-level-design.md (Goals — import/export parity; Success Metrics — falsification signal on a silently dropped card)

### LLD
- docs/intent/import-export/import-export-design.md

### EARS
- docs/intent/import-export/import-export-specs.md (PORT-001..008, PORT-020..022)

### Tests
- (M2) backend/internal/deckio/parse_test.go, backend/internal/api/imports_test.go

### Code
- (M2) backend/internal/deckio/ (parsers + emitters), backend/internal/api/imports.go, backend/internal/db/migrations/*_create_imports.up.sql, backend/internal/db/queries/imports.sql

## Architecture

**Purpose:** get a deck in from and out to the community text formats, losslessly
or loudly lossy.

**Key components (M2):**
1. `imports` audit table (raw text + parsed result + unresolved lines).
2. `internal/deckio` — tolerant parsers for the four formats; plain-text + MTGA
   emitters.
3. Name resolution against `cards` — exact then fuzzy; split/DFC face → whole
   card; unmirrored `(SET) collector#` → `CARD-009` fallback.
4. Parse-then-apply endpoints — `POST /import` (parse + audit, no write),
   `POST /import/{id}/apply` (one transaction, `DECK-060`), `GET /export`.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Import / export | PORT-001..008 | 0 | 0 | 8 (M2) |
| Legacy formats / paste-to-new-deck / printing export | PORT-020..022 | 0 | 3 | 0 |

**Summary:** 0 of 11 implemented; 8 gaps (M2); 3 deferred.

## Key Findings

1. Import is parse-then-apply with an audit row between — the user sees what
   resolved before committing 100 cards.
2. An unresolved name is reported and stored, never dropped — a named HLD
   falsification signal.
3. Archidekt `[Category]` tags are captured into `deck_cards.category`,
   reinforcing the functional-category decision.

## Work Required

### Must Fix
(none — no M1 scope)

### Should Fix
1. Implement PORT-001..008 at M2.

### Nice to Have
1. `.dec` / `.cod` (PORT-020); paste-to-new-deck (PORT-021).
