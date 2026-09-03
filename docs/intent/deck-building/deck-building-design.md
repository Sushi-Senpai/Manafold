---
parent: high-level-design
prefix: DECK
---

# Deck Building

## Context and Design Philosophy

The core of Manafold: a deck is a set of card *entries*, each with a quantity, a
board assignment, and a functional category, plus a designated commander (and
optionally a second/partner commander). Commander legality — colour identity,
singleton, the 100-card count, the banlist, and commander shape — is validated by
the system every time the deck changes and surfaced as a live report, never left
to the user. This segment owns `decks`, `deck_cards`, the pure validator
`internal/deckrules`, the deterministic analyser `internal/deckstats`, deck
CRUD, and the read-only public deck view.

Ownership is enforced **in the SQL**, not by a per-handler check: every deck
query scopes to the caller's *owner key* — their user ID when authenticated, or
their anonymous-draft token when not (`account-access` resolves which, per
ACCT-020). The scope clause is `AND (decks.user_id = sqlc.narg(user_id) OR
decks.anon_token = sqlc.narg(anon_token))` (or a join to `decks` for `deck_cards`
rows) with exactly one of the two arguments non-null; because a comparison
against a `NULL` argument is itself `NULL`, this resolves to the single active
predicate. A request against a deck the caller does not own returns zero rows,
which the handler maps to `404` — identical for a wrong user and a wrong token.
`CreateDeck` likewise inserts whichever of `user_id` / `anon_token` is set,
satisfying the exactly-one-owner CHECK.

When an anonymous caller signs in, `account-access`'s claim endpoint (ACCT-021)
runs `ClaimAnonDecks`, a single `UPDATE decks SET user_id = $caller, anon_token
= NULL WHERE anon_token = $token`, transferring every draft in one statement
(DECK-041).

## Schema

Migration `create_decks` creates `decks` and `deck_cards`.

### `decks`

| column | type | notes |
|---|---|---|
| `id` | uuid pk | |
| `user_id` | uuid null fk → users on delete cascade | **nullable** — an anonymous draft has `NULL` until claimed (see `account-access`) |
| `anon_token` | text null | opaque token identifying an unclaimed draft's owner; `NULL` once `user_id` is set |
| `name` | text not null default `'Untitled deck'` | |
| `description` | text not null default `''` | plain markdown "primer" |
| `commander_card_id` | uuid null fk → cards | |
| `partner_card_id` | uuid null fk → cards | second commander (partner / background / friends-forever) |
| `format` | text not null default `'commander'` | leaves room for Brawl/Oathbreaker later |
| `bracket` | int null | self-assigned 1–5 |
| `power_estimate` | numeric null | Manafold's computed estimate (later) |
| `is_public` | bool not null default false | read-only share URL |
| `color_identity` | text[] not null default `'{}'` | denormalised = union of commander(s); recomputed on every commander change |
| `created_at`, `updated_at` | timestamptz not null default `now()` | shared `set_updated_at()` trigger |

Constraint: exactly one of `user_id` / `anon_token` is non-null
(`CHECK ((user_id IS NULL) <> (anon_token IS NULL))`).

### `deck_cards`

| column | type | notes |
|---|---|---|
| `id` | uuid pk | |
| `deck_id` | uuid not null fk → decks on delete cascade | |
| `card_id` | uuid not null fk → cards | legality is checked against this |
| `print_id` | uuid null fk → card_prints | chosen printing; `NULL` = default/newest |
| `quantity` | int not null default 1 check (> 0) | > 1 only legal for basics / `singleton_limit` cards |
| `board` | text not null check (in `command`, `main`, `maybe`, `sideboard`) | |
| `category` | text null | free-text functional category (Ramp / Draw / Removal / Board Wipe / Counterspell / Land / …) |
| `added_at` | timestamptz not null default `now()` | |
| unique | `(deck_id, card_id, board)` | one entry per card per board |

## `internal/deckrules` — the validator

A pure package: no DB access, no AI. `Validate(input ValidationInput)
ValidationReport`. The handler loads the deck, its cards' Oracle fields, and the
banlist overrides, builds the `ValidationInput`, and calls `Validate`.

`ValidationInput` carries: the deck's `color_identity`; the commander and partner
`CardFacts` (id, name, `color_identity`, `commander_color_identity`,
`can_be_commander`, `keywords`, `oracle_text`, `type_line`); and a slice of
entries, each `{ CardFacts, board, quantity }` plus per-card
`legalities_commander` and `override_banned *bool`.

`CardFacts` also carries `is_basic_land` (derived by the handler: `type_line`
contains "Basic" and "Land") and `singleton_limit *int`.

`Validate` produces a `ValidationReport`:

```
type ValidationReport struct {
    ColorIdentityViolations []ColorIdentityViolation // card outside deck identity
    SingletonViolations     []SingletonViolation     // card over its limit
    MainCommandCount        int
    CountDeviation          int                      // MainCommandCount - 100
    BanlistViolations       []BanlistViolation
    CommanderIssues         []string                 // shape problems
    Legal                   bool                     // no violations of any kind and CountDeviation == 0
}
```

Rules:

1. **Colour identity** — for every entry on `main` or `command`, the card's
   `color_identity` must be a subset of the deck's `color_identity`. Offenders
   are reported (not rejected). `maybe` / `sideboard` entries are not checked.
2. **Singleton** — for each `card_id` on `main` + `command`, summed `quantity`:
   `> 1` is a violation when the card is not a basic land and
   `singleton_limit` is `NULL`; `> N` when `singleton_limit == N`; never a
   violation when `singleton_limit == 0` or the card is a basic land.
3. **Count** — `MainCommandCount` = summed `quantity` over `main` + `command`.
   `CountDeviation` = that minus 100. Reported as a number, not a pass/fail on
   its own (`Legal` folds it in).
4. **Banlist** — an entry is a banlist violation when its `override_banned`
   points to `true`, or (`override_banned` is nil and
   `legalities_commander == "banned"`). An `override_banned` of `false`
   *un-bans* a card Scryfall still lists as banned.
5. **Commander shape** — `commander_card_id` must be set;
   `can_be_commander` must be true for it. If `partner_card_id` is set, both
   cards must carry a compatible partner variant, parsed from `keywords` +
   `oracle_text`: plain "Partner" on both; "Partner with <name>" naming each
   other; "Friends forever" on both; one "Choose a Background" + one card whose
   `type_line` contains "Background"; one "Time Lord Doctor" + one "Doctor's
   companion". A mismatch is a `CommanderIssues` entry.

The deck's `color_identity` is recomputed by the **handler** (not the validator)
whenever a commander is assigned or cleared: the union of
`commander.commander_color_identity` and (if present)
`partner.commander_color_identity`, falling back to `color_identity` when
`commander_color_identity` is null.

## `internal/deckstats` — the analyser

A pure package: `Analyze(cards []CardStat) Stats`, no DB and no AI — the handler
assembles the input from already-loaded rows. It is deterministic; an LLM prose
summary over these numbers is a later milestone (M5).

`CardStat` per entry: `TypeLine`, `ManaCost` (Scryfall string), `ManaValue`,
`Quantity`, `IsLand`, `ProducedMana` (Scryfall `produced_mana`), `Category`.

`Stats` returns:

- **`TypeCounts`** — quantity by primary type (`Creature` / `Planeswalker` /
  `Land` / `Artifact` / `Enchantment` / `Instant` / `Sorcery` / `Battle` /
  `Other`), checked in that order so "Legendary Creature — God" lands under
  Creature.
- **`AvgManaValue`** — mean mana value of the non-land cards.
- **`ManaCurve`** — non-land quantity bucketed by integer mana value `0`–`6`,
  everything `≥ 7` collapsed to `7+`.
- **`ColorPips`** — coloured mana symbols demanded by non-land mana costs; each
  `{…}` symbol adds 1 to every WUBRG letter it names, so hybrid `{W/U}` counts
  for both and Phyrexian `{W/P}` counts for W.
- **`ColorSources`** — quantity of cards (of any board-counted type) whose
  `produced_mana` includes each WUBRG colour — the denominator the pip demand is
  read against.
- **`CategoryCounts`** — `deck_cards.category` rolled up: a value matching the
  known vocabulary (`KnownCategories`) or a synonym (`categorySynonyms`) is
  counted under its canonical name; anything else passes through verbatim
  (`DECK-052`).
- **`LandCount`** / **`NonLandCount`**.

`CategoryTargets` is an exported map of Commander rules-of-thumb bands (Ramp /
Card Draw / Removal 8–12, Board Wipe 3–5, Counterspell 0–8) the roll-up is meant
to be read against — guidance for the UI and `ai-assist`, not validation. The
stats endpoint echoes it alongside the counts. Land carries no band: the roll-up
only counts cards the user has manually tagged, so a land target would read
"under" for nearly every deck; the real land signal is `LandCount`, surfaced
separately in the land / non-land split.

The analyser looks only at the `main` and `command` boards, matching what
`/validation` counts.

## HTTP API

All under the protected `/api` group (M1: `DevAuth`). Registered via
`registerDeckRoutes(r, h)` (`PLATFORM-005`).

| Endpoint | Method | Body | Response | Notes |
|---|---|---|---|---|
| `/api/decks` | POST | `{ name? }` | `Deck` | owned by the caller; `format = commander`; no commander |
| `/api/decks` | GET | — | `Deck[]` | the caller's decks |
| `/api/decks/{id}` | GET | — | `DeckDetail` | entries grouped by board and category, each with Oracle data + chosen/newest printing; `404` if not owned (unless public — see DECK-030) |
| `/api/decks/{id}/commander` | PUT | `{ commander_card_id, partner_card_id? }` | `DeckDetail` | rejects `422` if `can_be_commander` is false; recomputes `color_identity` |
| `/api/decks/{id}/cards` | POST | `{ card_id, board, quantity?, category?, print_id? }` | `DeckCard` (flagged) | increments an existing `(deck_id, card_id, board)` entry rather than duplicating; the response carries any colour-identity / singleton flag for that entry |
| `/api/decks/{id}/cards/{cardId}` | DELETE | — | `204` / `404` | scoped through `decks.user_id`; `204` only when a row was deleted, `404` when none matched |
| `/api/decks/{id}/validation` | GET | — | `ValidationReport` | |
| `/api/decks/{id}/stats` | GET | — | `Stats` + `category_targets` | deterministic; curve / pips / sources / category roll-up over `main` + `command` (`DECK-051`, `DECK-052`) |
| `/api/decks/{id}` | PATCH | `{ name?, description?, is_public?, bracket? }` | `Deck` | |
| `/public/decks/{id}` | GET | — | `DeckDetail` (read-only) | mounted at the router root, outside the `/api` auth group; unauthenticated; `404` (never `401`) unless `is_public` |

Adding a card **records the entry and returns it flagged** when the card is
outside colour identity — it does not silently reject or silently accept
(`DECK-004`). The full report is always available from `/validation`.

## Frontend

- **`/decks`** — the caller's decks as cards, plus a "New deck" action that
  `POST`s and routes to the builder.
- **`/decks/[id]`** — the builder:
  - **Commander picker** — an autocomplete (`/api/cards/autocomplete`) filtered
    to `is:commander` results; selecting one `PUT`s `/commander`.
  - **Card-search box** — `/api/cards/search`; results list with an "Add"
    action per row that `POST`s `/cards` (board `main`).
  - **Decklist** — entries grouped by board, then by `category`, each row
    showing the card and any per-entry violation flag.
  - **Validation strip** — a persistent bar reading the `/validation` report:
    "2 cards outside colour identity", "97/100", "singleton: 2× Sol Ring",
    "banned: Channel". Refetched after every mutation.
  - **Deck stats panel** — reads `/stats`: land / non-land / average MV, a
    bar-chart mana curve, colour pips vs sources, and category counts against
    the rules-of-thumb bands. Refetched when the deck's cards or commander
    change.
  - **Import / export panel** — see `import-export`.
- `frontend/src/lib/deck.ts` — pure helpers: `groupByBoard`,
  `groupByCategory`, `formatValidationStrip(report)`.
- `frontend/src/lib/deckstats.ts` — pure view helpers over the stats payload:
  `curveRows`, `pipRows`, `categoryRows`.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Ownership enforcement | Scoped in every query to a polymorphic owner key (`decks.user_id = narg(user_id) OR decks.anon_token = narg(anon_token)`, exactly one non-null, or a join for `deck_cards`); zero rows → `404` | A per-handler `if deck.UserID != caller` check; two parallel query sets (one keyed by user, one by token) | The check cannot be forgotten on a new endpoint if it lives in the query, and "not found" and "not yours" collapse to one indistinguishable response. One `OR` predicate over both owner columns keeps a single query per operation instead of doubling the surface for the anonymous-draft case. |
| Card outside colour identity on add | Record the entry, return it flagged, surface it in `/validation` | Reject the add with `422`; accept silently | A builder needs to let you add a card and *see* it is illegal while you decide — rejecting outright blocks legitimate mid-build states; accepting silently defeats the point of the product. |
| Colour-identity computation | Store Scryfall's per-card `color_identity`; the deck's identity is the union of its commander(s)' `commander_color_identity`, recomputed by the handler on commander change | Recompute the deck identity on every validation call; compute per-card identity ourselves | Per-card identity is `card-data`'s job (verbatim from Scryfall). The deck identity changes only on a commander change, so recomputing it then and denormalising onto `decks` keeps validation a pure function of already-loaded data. |
| Validator purity | `internal/deckrules` takes plain structs, no DB, no AI; the handler assembles the input | The validator queries the DB itself | A pure function is exhaustively table-testable (partner identity, DFC identity, `singleton_limit` cards, basics, banlist, count) with no fixtures, and `ai-assist` can reuse it to gate model output without a DB round trip. |
| Banlist override semantics | `override_banned` tri-state: `true` bans, `false` un-bans a Scryfall-banned card, `nil` defers to Scryfall | Overrides can only *add* bans | The gap the table covers runs both ways — the Panel un-bans cards too, and Scryfall's next refresh lags. |
| Partner detection | Parse partner variants from `keywords` + `oracle_text` at validation time | A `partner_type` column derived at sync | Partner wording is stable and rare; parsing it in the validator keeps `card-data` from carrying deck-shape logic, and the five variants are a small closed set. |
| `deck_cards` uniqueness | `unique (deck_id, card_id, board)`; add increments `quantity` | One row per physical copy | Quantity is only ever > 1 for basics and `singleton_limit` cards; a row-per-copy model multiplies rows for no query benefit. |

## Open Questions & Future Decisions

### Deferred
1. **Deck-bootstrapping wizard** (captain bonus #1) — given a commander,
   auto-populate a mana base from the colour identity, then walk a wizard adding
   the most powerful/common cards for those colours. Needs: a "generate mana
   base" endpoint (basics + staple lands by `edhrec_rank` within identity), a
   wizard-state model (which slots are filled), and a hand-off to `ai-assist`
   for the "top cards by colour" step. The `deck_cards` model already supports
   it — bulk inserts with `category = 'Land'`. Related to roadmap M8.
2. **Functional-subtype grouping as first-class** (captain bonus #4) — the
   `category` field is free-text in M1; the full auto-categorizer (Oracle-text
   heuristics + Scryfall `oracle_tags`) and a builder UI that groups by
   Removal / Counterspell / Ramp / Card Draw / Board Wipe / Land / … land at
   M2/M5. The category vocabulary should be a known enum-like set with free-text
   as an escape hatch, and `deckstats` should roll counts up by it against the
   Commander rules-of-thumb.
3. **Low-friction swap UX** (captain bonus #5) — a "replace this card"
   interaction in the builder that removes one entry and adds another in one
   move, preserving `board` and `category`, and (when `ai-assist` is live)
   pre-filling the replacement search with suggested cuts' replacements. Needs a
   `PUT /api/decks/{id}/cards/{cardId}/replace` endpoint or a client-side
   remove+add; no schema change.
4. **Printing selection** — `print_id` is in the schema and defaults to
   newest; the picker UI is still pending. Its arrival unblocks `PORT-008` /
   `CARD-009` (pin an imported list's exact printings) and `PORT-022`
   (export chosen printings).
5. **Cut suggestions** (captain bonus #5, paired with the swap UX below) — once
   `deckstats` and `ai-assist` are both live, a "what should I cut" pass that
   reads the category roll-up against `CategoryTargets`, the curve, and
   `edhrec_rank` to propose the weakest entries. Design note only; roadmap M5+.
6. **Deck version history / named snapshots** — a `deck_snapshots` table with a
   jsonb entry list; M7. Not scaffolded now beyond this note.
7. **LLM deck-health prose** — `deckstats` numbers are deterministic; an
   `ai-assist` summary that reads them into a prioritised fix list is roadmap
   M5.

### Gaps
8. **`maybe` and `sideboard` boards are stored but not colour-identity checked**
   — intentional (they are staging areas), but the builder should make clear
   that a `maybe` card's legality is unchecked until moved to `main`.
9. **Concurrent edits to one deck** — last write wins; no optimistic
   concurrency token in v1. Acceptable for a single-user builder.
10. **Category auto-categorizer** — `deck_cards.category` is still hand-entered
    (or carried in from an Archidekt import). The Oracle-text + Scryfall
    `oracle_tags` heuristic categorizer that would populate it automatically
    (feeding `DECK-052`'s roll-up) is roadmap M5 and depends on `CARD-010`.

## References

- Code: `backend/internal/deckrules/`, `backend/internal/deckstats/`,
  `backend/internal/api/decks.go`, `backend/internal/api/stats.go`,
  `backend/internal/db/migrations/000003_create_decks.up.sql`,
  `backend/internal/db/queries/decks.sql`
- Tests: `backend/internal/deckrules/deckrules_test.go`,
  `backend/internal/deckstats/deckstats_test.go`,
  `backend/internal/api/decks_test.go`, `backend/internal/api/stats_test.go`
- Frontend: `frontend/src/app/(app)/decks/page.tsx`,
  `frontend/src/app/(app)/decks/[id]/page.tsx`, `frontend/src/lib/deck.ts`,
  `frontend/src/lib/deckstats.ts`
- Cross-segment: reads `card-data` (`cards`, `card_prints`, `banlist_overrides`);
  `user_id` / `anon_token` ownership comes from `account-access` (M1: the
  `DevAuth` user). `ai-assist` reuses `internal/deckrules` to gate model output.
  `import-export` writes `deck_cards` in bulk from a parsed decklist.
