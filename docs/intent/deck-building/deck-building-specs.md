# Deck Building — EARS Specs

## Deck CRUD

- [x] **DECK-001**: When an authenticated client creates a deck, the system shall persist it owned by that caller with `format = 'commander'` and no commander assigned.
- [x] **DECK-002**: When a client assigns a commander to a deck, if the chosen card's `can_be_commander` is false, then the system shall reject the assignment with `422` and leave the deck's commander unchanged.
- [x] **DECK-003**: When a commander or partner commander is assigned or cleared, the system shall recompute and store the deck's `color_identity` as the union of its commander(s)' commander colour identities.
- [x] **DECK-004**: When a client adds a card to a deck's `main` or `command` board whose `color_identity` is not a subset of the deck's `color_identity`, the system shall record the entry and return it flagged as a colour-identity violation, rather than silently rejecting or silently accepting it.
- [x] **DECK-005**: When a client adds a card already present on the same board, the system shall increment the existing entry's quantity rather than create a second row.
- [x] **DECK-006**: While a card's `singleton_limit` is `NULL` and the card is not a basic land, the system shall treat a total quantity above 1 across the `main` and `command` boards as a singleton violation; a `singleton_limit` of `N` raises that threshold to `N`; a `singleton_limit` of `0` imposes no limit; basic lands are never a singleton violation.
- [x] **DECK-007**: When a client requests a deck, the system shall return its entries grouped by board and by category, each entry carrying the card's Oracle data and its chosen printing, or its newest printing when none is chosen.
- [x] **DECK-008**: The system shall compute, for any deck, a validation report listing colour-identity violations, singleton violations, the `main`+`command` quantity total and its signed deviation from 100, and any entry whose `legalities.commander` is `banned` (unless a `banlist_overrides` row un-bans it) or whose `banlist_overrides` row bans it.
- [x] **DECK-009**: When a client mutates or reads a deck it does not own, the system shall respond `404`, with ownership enforced in the query rather than by a separate check.
- [x] **DECK-010**: When a client removes a card entry from a deck, the system shall scope the deletion through the deck's owner, respond `204` only when a row was actually deleted, and respond `404` when no row matched — whether because the deck is not owned by the caller or the card entry was absent.
- [x] **DECK-011**: When a client updates a deck's name, description, public flag, or bracket, the system shall persist only those fields and leave the deck's cards and commander untouched.
- [x] **DECK-012**: When a client sets an existing entry's quantity through `PATCH /api/decks/{id}/cards/{cardId}` with `{ board, quantity }`, the system shall, ownership-scoped in the query, set that `(deck_id, card_id, board)` entry's quantity when `quantity` is positive, delete the entry when `quantity` is `0`, reject a `quantity` that is negative or beyond a sane maximum with `400`, and respond `404` when no such entry exists — for a wrong board, an absent card, or a deck the caller does not own.
- [x] **DECK-013**: When the same `PATCH /api/decks/{id}/cards/{cardId}` request carries a `to_board` that differs from `board`, the system shall move that entry to `to_board` in one statement — carrying its quantity, printing, and category, and adding its quantity into any entry that already exists for the same card on `to_board` — and respond `404` when the source entry does not exist or the deck is not the caller's.

## Commander Shape

- [x] **DECK-020**: The validation report shall flag a deck whose `commander_card_id` is unset, or whose assigned commander's `can_be_commander` is false, as having a commander-shape issue.
- [x] **DECK-021**: When a deck has a `partner_card_id` set, the validation report shall flag a commander-shape issue unless both cards carry a compatible partner variant — plain "Partner" on both, "Partner with" naming each other, "Friends forever" on both, one "Choose a Background" plus one Background card, or one "Time Lord Doctor" plus one "Doctor's companion".

## Public Deck View

- [x] **DECK-030**: While a deck is marked public, the system shall serve a read-only view of it at `GET /public/decks/{id}` — mounted at the router root, outside the authenticated `/api` group — to unauthenticated clients, and shall expose no edit path for it to a non-owner.
- [x] **DECK-031**: While a deck is not marked public, `GET /public/decks/{id}` shall respond `404`, never `401`.

## Anonymous Drafts

- [x] **DECK-040**: Where a request carries an accepted anonymous-draft token and no valid session, the system shall scope deck creation, reads, and mutations to that token, mapping a deck the token does not own to `404` exactly as it does for an authenticated user.
- [x] **DECK-041**: When the claim endpoint runs for an authenticated caller holding an anonymous-draft token, the system shall, in one statement, set `user_id` to that caller and null `anon_token` on every deck currently owned by the token, and shall not touch decks owned by any other token or user.

## Deck Stats

- [x] **DECK-050**: The system shall provide a pure `internal/deckstats.Analyze` function returning per-deck type counts and average mana value.
- [x] **DECK-051**: The system shall compute, over the `main` and `command` boards, a per-deck mana curve (non-land cards bucketed by mana value, 0–6 then `7+`), colour-pip counts demanded by mana costs against colour-producing sources, and functional-category counts, and expose them with the Commander rules-of-thumb targets at `GET /api/decks/{id}/stats`.
- [x] **DECK-052**: The system shall treat the functional categories (Removal, Counterspell, Ramp, Card Draw, Board Wipe, Land, Protection, Recursion, Tutor, Threat) as a known vocabulary — matched case-insensitively and through a synonym table — that `deckstats` rolls counts up by, with any other `deck_cards.category` kept verbatim as a free-text escape hatch.

## Import Bulk-Add (owned with `import-export`)

- [x] **DECK-060**: When `import-export` supplies a parsed decklist, the system shall create `deck_cards` entries for every resolved card in one transaction, preserving each entry's board and category.

## Builder Card-Image Panel

- [x] **DECK-070**: The builder shall present a dedicated card-image panel fixed in the layout's left column and sticky within the layout, so it stays visible while the decklist and search lists scroll, showing exactly one card at a time.
- [x] **DECK-071**: The card-image panel shall render `image_uris.normal` when present, otherwise `image_uris.small`, otherwise a compact text card frame (name, mana cost, type line); it shall never mount an empty or broken image element.
- [x] **DECK-072**: When a card-name surface anywhere in the builder is hovered by the pointer or has keyboard focus, the panel shall show that card; when no card-name surface is hovered or focused, the panel shall show the deck's commander, or a neutral placeholder when the deck has no commander.
- [x] **DECK-073**: The system shall decode the panel's image before revealing it, keeping the previous image or a placeholder visible until decoding completes so the panel never flashes blank or broken.
- [x] **DECK-074**: When the pointer or focus moves directly from one card-name surface to an adjacent one, the panel shall hand over from the first card to the second without reverting to the commander in the gap between them.
- [x] **DECK-075**: On a device whose primary pointer does not support hover, the builder shall remain fully operable: the card-image panel then updates on keyboard focus of a card name and otherwise shows the commander or the placeholder.
- [x] **DECK-076**: Every card-name surface of the builder shall drive the card-image panel: each search result, each decklist entry, the assigned commander and partner, each commander-picker autocomplete option, and each AI-suggestion row.
- [D] **DECK-077**: When the card mirror exposes a distinct back-face image for a double-faced card, the panel shall offer a control to flip between faces; until then it shows the front face only. (`CardSummary` / `DeckEntry` carry one `image_uris` object, so a mirror change is the prerequisite.)
- [x] **DECK-078**: The builder shall deliver card previews only through the fixed panel — no floating tooltip and no preview element portalled outside the builder layout.

## Builder Search & Decklist UX

- [x] **DECK-080**: Each card-search result row shall identify the card by name, mana cost, type line, and colour identity.
- [x] **DECK-081**: The builder shall add a search result's card to the deck's `main` board from a single primary interaction — a click anywhere on the result row, or pressing Enter while that row holds the keyboard cursor — and shall offer adding the same card to the `sideboard` or `maybe` board only through a secondary control on the row that a row click does not trigger.
- [x] **DECK-082**: The card-search result list shall be keyboard navigable — ArrowDown and ArrowUp move a selection cursor over the rows (clamping at the ends), Home and End jump to the first and last row, and Enter adds the card of the row under the cursor.
- [x] **DECK-083**: When a search result's card already has entries anywhere in the deck, its row shall show the card's current total quantity across the deck rather than presenting it as absent.
- [x] **DECK-084**: When a card is added from a search result, that row shall give an explicit transient confirmation of the addition.
- [x] **DECK-085**: The card-search box shall pass its raw text to `GET /api/cards/search` unmodified, so the `id:` / `t:` / `cmc` / `o:` / `is:commander` predicates and bare terms keep working.
- [x] **DECK-086**: Each decklist entry on a non-`command` board shall present a quantity stepper — one control adds a copy, the other removes a copy (calling `PATCH …/cards/{cardId}` to set `quantity - 1`) and removes the entry when the last copy is taken — while the entry's colour-identity and singleton violation badges and its grouping stay visible.
- [x] **DECK-087**: The decklist shall group each board's entries by primary card type — Creatures, Instants, Sorceries, Artifacts, Enchantments, Planeswalkers, Battles, Lands, then any Other — showing the summed card count in each group header, keeping the board split (`main` / `sideboard` / `maybe`) as the outer level, and showing the commander as its own section above the boards.
- [x] **DECK-088**: The decklist's primary-type derivation shall read the front face of a `//` type line and classify it as the first match in the order Creature, Planeswalker, Land, Artifact, Enchantment, Instant, Sorcery, Battle, else Other — so a multi-type card such as "Artifact Creature — Golem" groups under Creature — mirroring `internal/deckstats`' type precedence.

## Builder Card Action Menu

- [x] **DECK-090**: The builder shall open a decklist row's action menu only from that row's dedicated `⋯` control and never on hover, and shall keep at most one action menu open at a time — opening one closes any other.
- [x] **DECK-091**: The action menu shall offer only actions valid for the entry in context: Add One, Add More, Remove One, Move to each board the entry is not already on, and Copy Card Name for every editable entry; Remove All only when the entry's quantity exceeds one.
- [x] **DECK-092**: At most one decklist row shall own the chords Alt+1 (Add One), Alt+2 (Remove One), Alt+3 (Move to Sideboard), and Alt+4 (Move to Considering) at a time — the row under the pointer, or, when no row is under the pointer, the row with keyboard focus within it (the pointer row wins when both are set). While the caller is not typing in a field, pressing a chord shall fire its action on that one owning row — whether or not any menu is open, and without any menu being rendered — matched on the physical digit key.
- [x] **DECK-093**: For the commander entry the action menu shall offer only Copy Card Name — no quantity or board actions — and the pointer/focus quantity and board chords shall do nothing.
- [x] **DECK-094**: An open action menu shall close on an outside click, on Escape, when the decklist scroll region scrolls, and on navigation away; and it shall be positioned so it neither covers its own row nor leaves the viewport, flipping above the row or clamping to the viewport edge as needed.

## Builder Layout

- [x] **DECK-095**: The builder shall not render the import/export panel inline; it shall be reached from a "Tools" control on the deck header that opens it in a dialog dismissible by Escape, an outside click, or a close control.
- [x] **DECK-096**: Above a narrow-viewport breakpoint the builder shall lay out as three columns — a sticky left column carrying the card-image panel and a collapsible deck-stats section, a center column for the type-grouped decklist, and a right column carrying AI suggestions at its top with card search below — and shall collapse to a single column on a narrow viewport; the legality validation summary shall be pinned (sticky) directly under the deck header, outside the three columns, so it remains visible while building at every breakpoint.
