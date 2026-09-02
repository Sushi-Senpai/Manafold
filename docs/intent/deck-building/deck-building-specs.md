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

## Commander Shape

- [x] **DECK-020**: The validation report shall flag a deck whose `commander_card_id` is unset, or whose assigned commander's `can_be_commander` is false, as having a commander-shape issue.
- [x] **DECK-021**: When a deck has a `partner_card_id` set, the validation report shall flag a commander-shape issue unless both cards carry a compatible partner variant — plain "Partner" on both, "Partner with" naming each other, "Friends forever" on both, one "Choose a Background" plus one Background card, or one "Time Lord Doctor" plus one "Doctor's companion".

## Public Deck View

- [x] **DECK-030**: While a deck is marked public, the system shall serve a read-only view of it at `GET /public/decks/{id}` — mounted at the router root, outside the authenticated `/api` group — to unauthenticated clients, and shall expose no edit path for it to a non-owner.
- [x] **DECK-031**: While a deck is not marked public, `GET /public/decks/{id}` shall respond `404`, never `401`.

## Anonymous Drafts

- [ ] **DECK-040**: Where a request carries no session but carries an anonymous-draft token, the system shall let the caller create and edit decks owned by that token.
- [ ] **DECK-041**: When a client signs in while holding an anonymous-draft token, the system shall reassign every deck owned by that token to the signed-in user and clear the token.

## Deck Stats

- [x] **DECK-050**: The system shall provide a pure `internal/deckstats.Analyze` function returning per-deck type counts and average mana value.
- [x] **DECK-051**: The system shall compute, over the `main` and `command` boards, a per-deck mana curve (non-land cards bucketed by mana value, 0–6 then `7+`), colour-pip counts demanded by mana costs against colour-producing sources, and functional-category counts, and expose them with the Commander rules-of-thumb targets at `GET /api/decks/{id}/stats`.
- [x] **DECK-052**: The system shall treat the functional categories (Removal, Counterspell, Ramp, Card Draw, Board Wipe, Land, Protection, Recursion, Tutor, Threat) as a known vocabulary — matched case-insensitively and through a synonym table — that `deckstats` rolls counts up by, with any other `deck_cards.category` kept verbatim as a free-text escape hatch.

## Import Bulk-Add (owned with `import-export`)

- [x] **DECK-060**: When `import-export` supplies a parsed decklist, the system shall create `deck_cards` entries for every resolved card in one transaction, preserving each entry's board and category.
