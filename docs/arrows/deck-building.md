# Arrow: deck-building

`decks` / `deck_cards`, the pure `internal/deckrules` validator, the pure
`internal/deckstats` analyser, deck CRUD, commander assignment, add/remove
cards, the validation report, the deterministic stats endpoint, the bulk import
write, and the read-only public deck view.

## Status

**MAPPED** — authored with the M1 slice (2026-09-02); deck stats (`DECK-051`,
`DECK-052`) and the import bulk-add (`DECK-060`) implemented at M2; polymorphic
owner-key scoping and `ClaimAnonDecks` for anonymous drafts (`DECK-040`,
`DECK-041`) implemented at M3. Builder-experience pass (2026-09-06): the
`PATCH …/cards/{cardId}` quantity/board-move endpoint (`DECK-012`, `DECK-013`)
and the frontend builder rebuild — shared card hover preview (`DECK-070..077`),
enriched keyboard-navigable card search (`DECK-080..086`), and the Moxfield-style
hover action menu + Alt+1..4 shortcuts (`DECK-090..093`).

## References

### HLD
- docs/high-level-design.md (Approach — Rules are structural; Success Metrics — falsification signals)

### LLD
- docs/intent/deck-building/deck-building-design.md

### EARS
- docs/intent/deck-building/deck-building-specs.md (DECK-001..013, DECK-020..021, DECK-030..031, DECK-040..041, DECK-050..052, DECK-060, DECK-070..077, DECK-080..086, DECK-090..093)

### Tests
- backend/internal/deckrules/deckrules_test.go — DECK-004, DECK-006, DECK-008, DECK-020, DECK-021 (partner identity, DFC identity, singleton_limit cards, basic lands, banlist, count)
- backend/internal/api/decks_test.go — DECK-001, DECK-002, DECK-003, DECK-004, DECK-005, DECK-008, DECK-009, DECK-010, DECK-012, DECK-013, DECK-030, DECK-031 (`TestPatchCard_QuantityAndBoardMove` — set quantity, 0 deletes, negative 400, board move carries + merges quantity, non-owner 404)
- frontend/src/lib/cardPreview.test.ts — DECK-071, DECK-072 (image source fallback; edge flip + viewport clamp)
- frontend/src/lib/searchNav.test.ts — DECK-082, DECK-083 (cursor movement + clamping; per-card in-deck quantity)
- frontend/src/lib/cardActions.test.ts — DECK-091, DECK-092, DECK-093 (context-filtered action list; Alt-chord mapping; commander limited to Copy Card Name)
- E2E (Chrome DevTools Protocol against headless Chromium, evidence in repo — see `docs/evidence/builder-previews/README.md`) — DECK-070, DECK-074, DECK-076, DECK-080, DECK-084, DECK-090: hover preview over a search result and a decklist row, enriched results, action menu
- backend/internal/api/stats_test.go — DECK-051, DECK-052 (curve / pips / sources / category roll-up end to end; non-owner 404)
- backend/internal/api/imports_test.go — DECK-060 (bulk write in one transaction, board + category preserved)
- backend/internal/api/auth_test.go — DECK-040, DECK-041 (anonymous deck create/read scoped to the token; claim reassigns and nulls the token)
- backend/internal/deckstats/deckstats_test.go — DECK-050, DECK-051 (curve buckets, hybrid/Phyrexian pip counting, source counts), DECK-052 (vocabulary + synonym roll-up, free-text passthrough)

### Code
- backend/internal/deckrules/ (`Validate`, partner-variant parsing)
- backend/internal/deckstats/ (`Analyze` — type counts, curve, pips vs sources, category roll-up, `CategoryTargets`)
- backend/internal/api/decks.go (`RegisterDeckRoutes` + all deck handlers, ownership scoped in queries to a polymorphic owner key — `user_id` OR `anon_token`; `patchCard` — DECK-012 / DECK-013)
- backend/internal/db/queries/decks.sql (`ClaimAnonDecks` — DECK-041; `SetDeckCardQuantity`, `MoveDeckCard` — DECK-012 / DECK-013)
- backend/internal/api/stats.go (`getDeckStats` — assembles `CardStat`s over main+command, echoes `CategoryTargets`)
- backend/internal/db/migrations/000003_create_decks.up.sql / .down.sql
- frontend/src/app/(app)/decks/page.tsx, frontend/src/app/(app)/decks/[id]/page.tsx (builder page: mounts the preview provider + the extracted panels; stats + import/export panels still inline)
- frontend/src/components/builder/ (CardPreviewContext, CardHoverPreview, useHoverPreview, HoverCardName, CommanderPicker, CardSearch, SearchResultList, SearchResultRow, ManaSymbols, Decklist, DecklistRow, QuantityStepper, CardActionMenu)
- frontend/src/lib/cardPreview.ts, frontend/src/lib/searchNav.ts, frontend/src/lib/cardActions.ts, frontend/src/lib/deck.ts, frontend/src/lib/deckstats.ts
- frontend/src/lib/api.ts (`setCardQuantity`, `moveCard`)

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
5. Builder frontend — componentised under `frontend/src/components/builder/`:
   commander picker, enriched keyboard-navigable card search, decklist grouped
   by board/category with a quantity stepper and a Moxfield-style hover action
   menu (Alt+1..4 shortcuts), a shared floating card hover preview on every
   card-name surface, live validation strip, deck stats panel, import/export
   panel. Placement / search-cursor / action-filtering logic is in pure
   `frontend/src/lib/` helpers with unit tests.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Deck CRUD | DECK-001..013 | 13 | 0 | 0 |
| Commander shape | DECK-020..021 | 2 | 0 | 0 |
| Public deck view | DECK-030..031 | 2 | 0 | 0 |
| Anonymous drafts | DECK-040..041 | 2 | 0 | 0 |
| Deck stats | DECK-050..052 | 3 | 0 | 0 |
| Import bulk-add | DECK-060 | 1 | 0 | 0 |
| Builder card previews | DECK-070..077 | 7 | 1 (DECK-077 DFC flip) | 0 |
| Builder search & decklist UX | DECK-080..086 | 7 | 0 | 0 |
| Builder card action menu | DECK-090..093 | 4 | 0 | 0 |

**Summary:** 41 of 42 implemented; DECK-077 (double-faced preview flip) deferred
on a `card-data` prerequisite; 0 gaps.

## Key Findings

1. Ownership is enforced in the SQL — every query scopes to a polymorphic owner
   key (`decks.user_id = narg(user_id) OR decks.anon_token = narg(anon_token)`,
   one non-null), so a new endpoint cannot forget the check, "not found" and
   "not yours" collapse to `404`, and the same predicate covers an authenticated
   user and an anonymous-draft token (DECK-009, DECK-040).
2. Adding an out-of-identity card records it flagged rather than rejecting —
   the builder must let you see an illegal state while you decide (DECK-004).
3. `internal/deckrules` is pure (no DB, no AI), so it is exhaustively
   table-testable and `ai-assist` can reuse it as its output gate.

## Work Required

### Must Fix
(none)

### Should Fix
(none)

### Nice to Have
1. Deck-bootstrapping wizard, functional-subtype auto-categorizer, low-friction
   swap UX, cut suggestions — captain bonus features, see the LLD Open
   Questions; each rides on `ai-assist` (M4+) and/or `CARD-010`.
