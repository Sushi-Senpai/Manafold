# Arrow: deck-building

`decks` / `deck_cards`, the pure `internal/deckrules` validator, the pure
`internal/deckstats` analyser, deck CRUD, commander assignment, add/remove
cards, the validation report, the deterministic stats endpoint, the bulk import
write, and the read-only public deck view.

## Status

**MAPPED** — authored with the M1 slice (2026-09-02); deck stats (`DECK-051`,
`DECK-052`) and the import bulk-add (`DECK-060`) implemented at M2.

## References

### HLD
- docs/high-level-design.md (Approach — Rules are structural; Success Metrics — falsification signals)

### LLD
- docs/intent/deck-building/deck-building-design.md

### EARS
- docs/intent/deck-building/deck-building-specs.md (DECK-001..011, DECK-020..021, DECK-030..031, DECK-040..041, DECK-050..052, DECK-060)

### Tests
- backend/internal/deckrules/deckrules_test.go — DECK-004, DECK-006, DECK-008, DECK-020, DECK-021 (partner identity, DFC identity, singleton_limit cards, basic lands, banlist, count)
- backend/internal/api/decks_test.go — DECK-001, DECK-002, DECK-003, DECK-004, DECK-005, DECK-008, DECK-009, DECK-010, DECK-030, DECK-031
- backend/internal/api/stats_test.go — DECK-051, DECK-052 (curve / pips / sources / category roll-up end to end; non-owner 404)
- backend/internal/api/imports_test.go — DECK-060 (bulk write in one transaction, board + category preserved)
- backend/internal/deckstats/deckstats_test.go — DECK-050, DECK-051 (curve buckets, hybrid/Phyrexian pip counting, source counts), DECK-052 (vocabulary + synonym roll-up, free-text passthrough)

### Code
- backend/internal/deckrules/ (`Validate`, partner-variant parsing)
- backend/internal/deckstats/ (`Analyze` — type counts, curve, pips vs sources, category roll-up, `CategoryTargets`)
- backend/internal/api/decks.go (`RegisterDeckRoutes` + all deck handlers, ownership scoped in queries)
- backend/internal/api/stats.go (`getDeckStats` — assembles `CardStat`s over main+command, echoes `CategoryTargets`)
- backend/internal/db/migrations/000003_create_decks.up.sql / .down.sql
- backend/internal/db/queries/decks.sql (`ListDeckCardEntries` now also selects `produced_mana`)
- frontend/src/app/(app)/decks/page.tsx, frontend/src/app/(app)/decks/[id]/page.tsx (builder: + stats panel, + import/export panel)
- frontend/src/lib/deck.ts, frontend/src/lib/deckstats.ts

## Architecture

**Purpose:** the core — build a legal Commander deck with live validation.

**Key components:**
1. Schema — `decks` (nullable `user_id` + `anon_token`, denormalised
   `color_identity`), `deck_cards` (`board` / `quantity` / `category`, unique on
   `(deck_id, card_id, board)`).
2. `internal/deckrules` — pure `Validate(ValidationInput) ValidationReport`:
   colour identity ⊆ deck identity, singleton with `singleton_limit` + basics,
   `main`+`command` count vs 100, banlist (`legalities` + overrides), commander
   shape + the five partner variants.
3. `internal/deckstats` — pure `Analyze`: type counts, non-land mana curve
   (`0`–`6`, `7+`), colour-pip demand vs colour sources, category roll-up over a
   known vocabulary + synonyms with free-text passthrough, `CategoryTargets`
   rules-of-thumb bands.
4. HTTP — CRUD + `PUT /commander` (422 when `can_be_commander` false) + add/remove
   cards (add returns the entry flagged, never silently rejects/accepts) +
   `GET /validation` + `GET /stats`; ownership scoped in every query → `404`; a
   read-only `GET /public/decks/{id}` (router root, outside `/api`) gated on
   `is_public`.
5. Builder frontend — commander picker, card search + add, decklist grouped by
   board/category, live validation strip, deck stats panel, import/export panel.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Deck CRUD | DECK-001..011 | 11 | 0 | 0 |
| Commander shape | DECK-020..021 | 2 | 0 | 0 |
| Public deck view | DECK-030..031 | 2 | 0 | 0 |
| Anonymous drafts | DECK-040..041 | 0 | 0 | 2 (M3) |
| Deck stats | DECK-050..052 | 3 | 0 | 0 |
| Import bulk-add | DECK-060 | 1 | 0 | 0 |

**Summary:** 19 of 21 implemented; 2 gaps, both anonymous drafts (DECK-040/041, M3).

## Key Findings

1. Ownership is enforced in the SQL (`AND decks.user_id = $n`), so a new
   endpoint cannot forget the check, and "not found" and "not yours" collapse to
   `404` (DECK-009).
2. Adding an out-of-identity card records it flagged rather than rejecting —
   the builder must let you see an illegal state while you decide (DECK-004).
3. `internal/deckrules` is pure (no DB, no AI), so it is exhaustively
   table-testable and `ai-assist` can reuse it as its output gate.

## Work Required

### Must Fix
(none)

### Should Fix
1. Anonymous drafts + claim-on-sign-in (DECK-040/041) — M3, with `account-access`.

### Nice to Have
1. Deck-bootstrapping wizard, functional-subtype auto-categorizer, low-friction
   swap UX, cut suggestions — captain bonus features, see the LLD Open
   Questions; each rides on `ai-assist` (M4+) and/or `CARD-010`.
