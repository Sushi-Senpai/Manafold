# Import / Export — EARS Specs

The parsers, emitters, and the parse-then-apply endpoints ship at M2. PORT-008
(the live single-printing fallback) waits on `card-data`'s CARD-009 and lands
with printing-selection UI.

- [x] **PORT-001**: When a client submits raw decklist text with a declared source format, the system shall parse it into resolved entries and unresolved lines, store an `imports` audit row (raw text, parsed result, unresolved lines), and return both lists without yet writing any `deck_cards`.
- [x] **PORT-002**: The system shall parse the plain-text, MTG Arena, Moxfield-paste, and Archidekt-paste decklist formats, recognising `Commander` / `Deck` / `Sideboard` / `Maybeboard` section headers (tolerating a trailing `(N)` count and an optional colon) and mapping them to the `command` / `main` / `sideboard` / `maybe` boards, and for the Arena/Moxfield paste with no explicit headers treating the first blank line after an entry as the main/sideboard divider.
- [x] **PORT-003**: When an Archidekt-paste line carries an inline `[Category]` or `[Category{modifier}]` tag, the system shall capture that category name into the entry's `deck_cards.category`.
- [x] **PORT-004**: When a parsed line's card name resolves to no card in the mirror, the system shall place it in the unresolved list with its original text and shall never silently drop it; a line the grammar cannot parse at all is reported separately as rejected.
- [x] **PORT-005**: When a parsed line names one face of a split or double-faced card, the system shall resolve it to the whole card.
- [x] **PORT-006**: When a client applies a stored import, the system shall create `deck_cards` entries for every resolved line in one transaction, preserving each entry's board and category (fulfils `deck-building`'s DECK-060), and shall mark the import applied.
- [x] **PORT-007**: When a client exports a deck as `plaintext` or `mtga`, the system shall emit a decklist with a `Commander` section and the mainboard (and `Maybeboard` / `Sideboard` when non-empty), one `<qty> <name>` entry per line sorted by name, in the requested format's conventions — plain text carries no printing reference, MTGA carries each entry's `(SET) collector#` when known.
- [ ] **PORT-008**: When an import line carries a set code and collector number not present in `card_prints`, the system shall use `card-data`'s single-printing Scryfall fallback (`CARD-009`) to mirror that printing before resolving the line. (Gap through M2: the line resolves by name only; the `(SET) collector#` is captured but advisory. Lands with CARD-009 and printing-selection UI.)

## Deferred

- [D] **PORT-020**: The system shall read and write the legacy `.dec` XML and `.cod` (Cockatrice) formats.
- [D] **PORT-021**: The system shall support creating a new deck directly from a pasted decklist in one step from the `/decks` page.
- [D] **PORT-022**: When exporting after printing selection exists, the system shall emit each entry's chosen `(SET) collector#`.
