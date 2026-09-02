---
parent: high-level-design
prefix: PORT
---

# Import / Export

# NOTE: no import/export code ships in M1. This LLD and its specs are drafted now;
# implementation begins at M2.

## Context and Design Philosophy

Manafold reads and writes the community's decklist text formats so a user can
bring a deck in from Moxfield/Archidekt/Arena and take one back out. The
community lingua franca is one entry per line — `<qty> <Card Name>` — with
optional `(<SET>) <collector#>` and section headers. Manafold parses a tolerant
superset and emits the plain-text and Arena forms.

Every import is an auditable two-step: parse the raw text into a structured
result (resolved entries + unresolved names), then, on the user's confirmation,
write `deck_cards` in one transaction (`DECK-060`). The raw text, the parsed
result, and the unresolved names are stored in an `imports` row so a bad parse
is diagnosable rather than silently lossy.

## Schema (migration `create_imports`, M2)

`imports` — `id`, `deck_id uuid null fk → decks on delete set null`,
`source_format` (`plaintext` / `mtga` / `moxfield` / `archidekt`), `raw_text`
text, `parsed jsonb`, `unresolved jsonb`, `created_at`.

## Formats

| Format | Read | Write | Notes |
|---|---|---|---|
| Plain text / "decklist" | yes | yes | `1 Sol Ring` per line; headers `Commander:` / `Deck` / `Sideboard` / `Maybeboard` |
| MTG Arena (MTGA) | yes | yes | `4 Lightning Bolt (2XM) 129`; blank line separates main from sideboard; split cards `A // B` |
| Moxfield paste | yes | — | its own line format with set, collector number, foil flag, tags |
| Archidekt paste | yes | — | `Commander` / `Deck` sections; tolerates inline `[Category]` / `[Category{top}]` tags — the category is captured into `deck_cards.category` |
| `.dec` XML, `.cod` (Cockatrice), collection CSV | — | — | out of scope |

## Name Resolution

Each parsed line's name is resolved against `card-data`'s `cards` table:
exact (case-insensitive) match first, then a fuzzy fallback (trigram similarity
over `name`, threshold tuned to avoid false matches). A line naming one face of a
split/DFC card (`Fire` for `Fire // Ice`) resolves to the whole card. A set +
collector number that is not in `card_prints` triggers `card-data`'s
single-printing Scryfall fallback (`CARD-009`). A name that resolves to nothing
goes into `unresolved` with its original text and is reported to the user; it is
never silently dropped (`PORT-004`, an HLD falsification signal).

## Endpoints (M2)

| Endpoint | Method | Body | Response |
|---|---|---|---|
| `/api/decks/{id}/import` | POST | `{ source_format, raw_text }` | `{ import_id, resolved: ParsedEntry[], unresolved: UnresolvedLine[] }` — parses, stores the `imports` row, does **not** write `deck_cards` yet |
| `/api/decks/{id}/import/{importId}/apply` | POST | `{ }` | `DeckDetail` — writes every resolved entry in one transaction (`DECK-060`) |
| `/api/decks/{id}/export` | GET | `?format=plaintext\|mtga` | `text/plain` decklist |

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Import as parse-then-apply | Two endpoints; an `imports` audit row between them | One endpoint that parses and writes in a single call | The user needs to see what resolved and what did not before committing 100 cards to a deck; the audit row makes a bad parse diagnosable after the fact. |
| Unresolved names | Reported explicitly in the response and stored; never dropped | Best-effort: import what resolves, ignore the rest | Silently dropping a resolvable card is a named HLD falsification signal — a decklist round-trip must be lossless or loudly lossy. |
| Formats written | Plain text + MTGA only | Also Moxfield/Archidekt's own formats | Plain text and Arena cover "get my deck into any other tool"; the vendor formats are read-only because their round-trip value is low and their specs drift. |
| Category tags on Archidekt import | Captured into `deck_cards.category` | Discarded | Reinforces the functional-category decision — an imported Archidekt deck keeps its Ramp/Draw/Removal grouping. |

## Open Questions & Future Decisions

### Deferred
1. **`.dec` / `.cod` / collection CSV** — low round-trip value; revisit if
   users ask.
2. **Bulk paste on the `/decks` page** ("new deck from a paste") — a create +
   import in one step; a UX convenience over the two endpoints above.
3. **Export with chosen printings** — once printing selection exists (M2), the
   MTGA export should emit each entry's `(SET) collector#`.

### Gaps
4. **Fuzzy-match threshold** — tuned empirically against a corpus of real
   pasted lists; a too-loose threshold turns a typo into the wrong card, a
   too-tight one under-resolves. The `imports` audit rows are the tuning data.

## References

- Code (M2): `backend/internal/deckio/` (parsers + emitters),
  `backend/internal/api/imports.go`,
  `backend/internal/db/migrations/*_create_imports.up.sql`,
  `backend/internal/db/queries/imports.sql`
- Cross-segment: resolves names against `card-data`'s `cards`; writes
  `deck-building`'s `deck_cards` via `DECK-060`; may trigger `CARD-009`.
