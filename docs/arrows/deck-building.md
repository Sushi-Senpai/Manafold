# Arrow: deck-building

`decks` / `deck_cards`, the pure `internal/deckrules` validator, the stubbed
`internal/deckstats`, deck CRUD, commander assignment, add/remove cards, the
validation report, and the read-only public deck view.

## Status

**MAPPED** — greenfield, authored with the M1 slice (2026-09-02).

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
- backend/internal/deckstats/deckstats_test.go — DECK-050

### Code
- backend/internal/deckrules/ (`Validate`, partner-variant parsing)
- backend/internal/deckstats/ (`Analyze` — counts + avg mana value stub)
- backend/internal/api/decks.go (`registerDeckRoutes` + all deck handlers, ownership scoped in queries)
- backend/internal/db/migrations/000002_create_decks.up.sql / .down.sql
- backend/internal/db/queries/decks.sql
- frontend/src/app/(app)/decks/page.tsx, frontend/src/app/(app)/decks/[id]/page.tsx
- frontend/src/lib/deck.ts

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
3. `internal/deckstats` — pure `Analyze`; M1 stub (type counts + avg MV).
4. HTTP — CRUD + `PUT /commander` (422 when `can_be_commander` false) + add/remove
   cards (add returns the entry flagged, never silently rejects/accepts) +
   `GET /validation`; ownership scoped in every query → `404`; a read-only
   `GET /public/decks/{id}` (router root, outside `/api`) gated on `is_public`.
5. Builder frontend — commander picker, card search + add, decklist grouped by
   board/category, live validation strip.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Deck CRUD | DECK-001..011 | 11 | 0 | 0 |
| Commander shape | DECK-020..021 | 2 | 0 | 0 |
| Public deck view | DECK-030..031 | 2 | 0 | 0 |
| Anonymous drafts | DECK-040..041 | 0 | 0 | 2 (M3) |
| Deck stats | DECK-050..052 | 1 | 0 | 2 (M2) |
| Import bulk-add | DECK-060 | 0 | 0 | 1 (M2) |

**Summary:** 16 of 21 implemented; 5 gaps (anonymous drafts M3, real stats M2, import bulk-add M2).

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
1. Real deck stats (DECK-051/052) — M2.

### Nice to Have
1. Deck-bootstrapping wizard, functional-subtype grouping, low-friction swap UX
   — captain bonus features, see the LLD Open Questions.
