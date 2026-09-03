---
parent: high-level-design
prefix: PORT
---

# Import / Export

## Context and Design Philosophy

Manafold reads and writes the community's decklist text formats so a user can
bring a deck in from Moxfield/Archidekt/Arena and take one back out. The
community lingua franca is one entry per line — `<qty> <Card Name>` — with
optional `(<SET>) <collector#>` and section headers. Manafold parses a tolerant
superset and emits the plain-text and Arena forms.

Every import is an auditable two-step: parse the raw text into a structured
result (resolved entries + unresolved names + unreadable lines), then, on the
user's confirmation, write `deck_cards` in one transaction (`DECK-060`). The raw
text, the parsed result, and the unresolved names are stored in an `imports` row
so a bad parse is diagnosable rather than silently lossy.

`internal/deckio` is a pure package — text in, structured lines out, and
structured entries back to text. It performs no name resolution and no database
access; resolving names against the mirror and the audited write live in
`internal/api/imports.go`. That split keeps the tolerant grammar exhaustively
table-testable.

## Schema (migration `create_imports`)

`imports` — `id`, `deck_id uuid null fk → decks on delete set null`,
`source_format` (`plaintext` / `mtga` / `moxfield` / `archidekt`, checked),
`raw_text` text, `parsed jsonb` (the full `deckio.ParseResult`),
`unresolved jsonb` (the lines that matched no card), `applied_at timestamptz null`
(set when the import is applied), `created_at`.

## Formats

| Format | Read | Write | Notes |
|---|---|---|---|
| Plain text / "decklist" | yes | yes | `1 Sol Ring` per line; headers `Commander:` / `Deck` / `Sideboard` / `Maybeboard` |
| MTG Arena (MTGA) | yes | yes | `4 Lightning Bolt (2XM) 129`; first blank line after an entry separates main from sideboard; `SB:`-prefixed lines and a `Companion` header also route to the sideboard |
| Moxfield paste | yes | — | its own line format with set, collector number, and a trailing foil flag (`*F*` / `^Foil^`), which the parser strips |
| Archidekt paste | yes | — | `Commander (1)` / `Deck (99)` headers with counts; inline `[Category]` / `[Category{top}]` tags — the category name is captured into `deck_cards.category` |
| `.dec` XML, `.cod` (Cockatrice), collection CSV | — | — | out of scope (`PORT-020`) |

One tolerant line grammar serves all four: `<qty>[x] <name>` with optional
trailing `[Category]`, foil marker, and `(SET) collector#`, peeled in that
order. A line the grammar cannot read is reported as *rejected* — distinct from a
line that parses but whose name does not resolve.

## Name Resolution

Each parsed line's name is resolved against `card-data`'s `cards` table by an
exact case-insensitive match on the whole name, or a match on one face of a
split / double-faced card (`Fire` for `Fire // Ice`), preferring the exact
whole-name match (`PORT-005`). A name that resolves to nothing goes into
`unresolved` with its original text and is reported to the user; it is never
silently dropped (`PORT-004`, an HLD falsification signal).

A captured set code + collector number is advisory in this milestone: the line
still resolves by name, and `card_prints` is not consulted for an exact
printing. The live single-printing fallback (`PORT-008` / `card-data`'s
`CARD-009`) lands with printing-selection UI.

## Endpoints

All under the protected `/api` group, registered via `RegisterImportRoutes`
(`PLATFORM-005`) beside the deck routes — import/export is its own arrow
segment.

| Endpoint | Method | Body | Response |
|---|---|---|---|
| `/api/decks/{id}/import` | POST | `{ source_format, raw_text }` | `{ import_id, resolved: ResolvedLine[], unresolved: UnresolvedLine[], rejected: string[] }` — parses, stores the `imports` row, does **not** write `deck_cards` |
| `/api/decks/{id}/import/{importId}/apply` | POST | — | `DeckDetail` — re-resolves the stored parse and writes every resolved entry in one transaction (`DECK-060`), then marks the import applied |
| `/api/decks/{id}/export` | GET | `?format=plaintext\|mtga` | `text/plain` decklist |

Ownership is scoped in the queries: the import insert draws `deck_id` from a
`decks` row filtered by owner, and the apply lookup joins `imports` to `decks`
by owner, so a request against a deck the caller does not own returns `404`
(`DECK-009`).

## Frontend

`frontend/src/app/(app)/decks/[id]/page.tsx` carries an **Import / export**
panel: a format `<select>`, a paste `<textarea>`, and a "Preview import" action
that calls `POST /import` and shows the resolved count, the unresolved names,
and an "Add N cards" button that calls `apply`. Two export buttons fetch the
plain-text and Arena forms into a read-only `<textarea>`.
`frontend/src/lib/deckstats.ts` is unrelated (deck stats); import/export view
state is local to the panel.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Import as parse-then-apply | Two endpoints; an `imports` audit row between them | One endpoint that parses and writes in a single call | The user needs to see what resolved and what did not before committing 100 cards to a deck; the audit row makes a bad parse diagnosable after the fact. |
| `internal/deckio` purity | Text ⇄ structured only; resolution and DB writes in `internal/api` | deckio resolves names and writes `deck_cards` itself | A pure grammar is exhaustively table-testable with no fixtures, and `ai-assist` can reuse the emitters without a DB. |
| One grammar for four formats | A single tolerant line parser; formats differ only in header handling | A dedicated parser per format | The four share `<qty> <name> [(SET) #] [tags]`; a shared tokenizer with per-format header rules is far less code than four near-duplicates whose vendor specs drift. |
| Unresolved vs rejected | Two separate lists — a name that matched nothing, versus a line the grammar could not read | One "problems" list | They have different fixes: a rejected line is a formatting problem the user edits; an unresolved name may be a real card missing from the mirror. |
| Unresolved names | Reported explicitly in the response and stored; never dropped | Best-effort: import what resolves, ignore the rest | Silently dropping a resolvable card is a named HLD falsification signal — a decklist round-trip must be lossless or loudly lossy. |
| Formats written | Plain text + MTGA only | Also Moxfield/Archidekt's own formats | Plain text and Arena cover "get my deck into any other tool"; the vendor formats are read-only because their round-trip value is low and their specs drift. |
| Category tags on Archidekt import | Captured into `deck_cards.category` | Discarded | Reinforces the functional-category decision — an imported Archidekt deck keeps its Ramp/Draw/Removal grouping, which `internal/deckstats` then rolls up (`DECK-052`). |
| Commander section on import | Written as `command`-board `deck_cards` rows | Also set `decks.commander_card_id` from the first `Commander` line | Assigning the validated commander runs colour-identity recomputation and a `can_be_commander` check (`DECK-002`, `DECK-003`); keeping it an explicit step after import avoids a surprising side effect and a half-set identity. Revisit as a convenience (see Open Questions). |

## Open Questions & Future Decisions

### Deferred
1. **`.dec` / `.cod` / collection CSV** (`PORT-020`) — low round-trip value;
   revisit if users ask.
2. **Bulk paste on the `/decks` page** (`PORT-021`) — a create + import in one
   step; a UX convenience over the two endpoints above.
3. **Export with chosen printings** (`PORT-022`) — once printing selection
   exists, the MTGA export should emit each entry's chosen `(SET) collector#`
   rather than its newest printing's.

### Gaps
4. **Auto-assign the commander on import** — an imported `Commander` line
   currently lands on the `command` board but does not set
   `decks.commander_card_id`. A follow-up could set it from the first
   `Commander` line when the deck has none and the card's `can_be_commander` is
   true.
5. **Fuzzy name matching** — resolution is exact-or-DFC-face only. A trigram
   (`pg_trgm`) similarity fallback would catch typos and punctuation drift in
   pasted lists; it needs the extension migration and an empirically tuned
   threshold (too loose turns a typo into the wrong card). The `imports` audit
   rows are the tuning corpus.
6. **`(SET) collector#` precision** (`PORT-008`) — captured but not yet used to
   pin an exact printing; blocked on `CARD-009`.

## References

- Code: `backend/internal/deckio/` (`Parse`, `EmitPlaintext`, `EmitMTGA`),
  `backend/internal/api/imports.go`,
  `backend/internal/db/migrations/000004_create_imports.up.sql` / `.down.sql`,
  `backend/internal/db/queries/imports.sql`,
  `backend/internal/db/queries/cards.sql` (`ResolveCardByName`)
- Tests: `backend/internal/deckio/parse_test.go`,
  `backend/internal/deckio/emit_test.go`,
  `backend/internal/api/imports_test.go`
- Frontend: `frontend/src/app/(app)/decks/[id]/page.tsx` (Import / export panel),
  `frontend/src/lib/api.ts` (`parseImport` / `applyImport` / `exportDeck`)
- Cross-segment: resolves names against `card-data`'s `cards`; writes
  `deck-building`'s `deck_cards` via `DECK-060`; will trigger `CARD-009` once
  that lands.
