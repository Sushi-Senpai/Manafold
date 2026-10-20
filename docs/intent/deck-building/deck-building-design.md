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
`internal/deckrules`, the deterministic analyser `internal/deckstats`, the
deterministic bracket check `internal/bracket`, deck CRUD, the archetype
templates (`internal/decktemplates`) with their role classifier
(`internal/deckroles`), and the read-only public deck view.

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
| `bracket` | int null | the owner's chosen bracket, 1–5; `NULL` = not chosen. The bracket check reads it as the target (`DECK-125`) |
| `archetype` | text null | a `decktemplates` template key (added by migration `add_deck_archetype`); `NULL` = no archetype chosen. Validated against the template set by the handler, not a DB constraint, so a new template needs no migration |
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

## Deck templates — target ranges per archetype

A *deck template* is a set of rough count ranges a Commander deck of a given
archetype should land in. Templates are guidance a player reads their deck
against — never validation — and they are the shared source of truth for both
the builder's "your deck vs. target" panel and the later deck-building wizard
(Deferred #1), which will read the very same data.

### `internal/decktemplates` — the one authoritative definition

A pure Go package holding the template data as code, stamped with an integer
`Version` that is bumped whenever any range changes, plus the evaluation
function. It is served verbatim at `GET /api/deck-templates`; the frontend never
carries its own copy of a range.

Each `Template` has a stable `key`, a display `name`, and an ordered list of
`rows`. A row is `{ key, label, kind, min, max }` (`kind` is `type` or `role`;
ranges are inclusive). Counts are out of the **99 non-commander cards** — the
`main` board only; the commander and partner are excluded, and planeswalkers
take their slots from the noncreature budget, so they have no row of their own.

Type rows (per archetype, captain-approved):

| key | Archetype | Lands | Creatures | Instants & sorceries | Artifacts | Enchantments |
|---|---|---|---|---|---|---|
| `go_wide_tribal` | Go-wide / Tribal | 36–38 | 28–35 | 10–14 | 8–11 | 4–8 |
| `voltron` | Voltron | 35–38 | 18–24 | 10–14 | 14–20 | 6–12 |
| `spellslinger` | Spellslinger | 34–38 | 10–16 | 28–36 | 8–12 | 2–6 |
| `midrange_value` | Midrange / Value | 36–38 | 25–32 | 14–20 | 8–12 | 4–8 |
| `control` | Control | 36–38 | 12–18 | 25–32 | 8–12 | 4–8 |
| `ramp_big_creatures` | Ramp / Big creatures | 38–40 | 22–28 | 12–18 | 10–14 | 3–7 |
| `artifacts` | Artifacts | 34–38 | 12–20 | 8–14 | 28–36 | 0–4 |
| `enchantress` | Enchantress | 36–38 | 12–18 | 10–14 | 6–10 | 24–32 |
| `aristocrats` | Aristocrats (sacrifice) | 36–38 | 30–38 | 10–14 | 8–12 | 4–8 |
| `lands_matter` | Lands-matter | 40–44 | 18–24 | 14–18 | 6–10 | 4–8 |

The land rule behind that column: 36–38 suits almost every deck; the top of a
land range is never below 38; the bottom may drop to 34 only for archetypes that
genuinely run lean (Spellslinger, Artifacts) or to 35 (Voltron); Ramp and
Lands-matter run above 38.

Role rows are shared by every archetype: **ramp 10–12, card draw 10–12,
targeted removal 8–10, board wipes 2–4** — except Control, whose board-wipe
target is **4–6**.

**Bracket 4 relaxation.** A deck in Bracket 4 running heavy fast mana may sit
below its land range, but not below 32 lands. The land row carries
`relaxed_at_bracket: 4` and `relaxed_floor: 32`; when the deck's self-assigned
`bracket` is exactly 4, a land count below the minimum but at or above 32
evaluates to `relaxed` instead of `under`. Below 32 is still `under`, and a
count above the maximum is still `over`. Bracket 5 gets no relaxation and reads
against the normal range, as does any bracket other than 4 or an unset one.
Manafold cannot yet tell whether a deck actually runs heavy fast mana, so the
relaxation keys on the player's own bracket claim alone; requiring the fast
mana is deferred until role tags can count it (Deferred #14). Any other row has
no relaxation.

### Counting

`Evaluate(template, counts, bracket)` returns one `RowFit` per template row:
`{ key, label, kind, count, min, max, status }` with `status` one of `ok`
(inclusive range), `under`, `over`, or `relaxed`. The handler supplies the
counts:

- **Type rows** come from `deckstats.Analyze` over the `main` board's entries —
  its `TypeCounts`, so a card counts under exactly one primary type with the
  same precedence (`Creature` → `Planeswalker` → `Land` → `Artifact` →
  `Enchantment` → `Instant` → `Sorcery` → `Battle` → `Other`) the stats panel
  uses. "Instants & sorceries" is `Instant + Sorcery`. An artifact creature
  therefore counts as a creature, not an artifact; a Dryad Arbor as a creature,
  not a land. There is no second type classifier.
- **Role rows** come from `internal/deckroles` (below), multiplied by quantity.
  A card may count toward several roles (a cantrip removal spell is both
  removal and draw).

### `internal/deckroles` — the role classifier

Roles are counted by a `Classifier` interface:

```go
type Classifier interface {
    Classify(c Card) []Role // Role ∈ ramp, card_draw, targeted_removal, board_wipe
}
type Card struct { TypeLine, OracleText string; Keywords []string; CardFaces []Face }
```

The handler and the template evaluation depend only on the interface, so the
deterministic default (`OracleClassifier`) can later be replaced by one backed
by Scryfall's community role tags (`oracle_tags`, `CARD-010`) without touching
the panel, the endpoint, or the templates. It does **not** read the free-text
`deck_cards.category` field.

`OracleClassifier` reads the card's Oracle text and type line — the top-level
fields, or, when the top-level Oracle text is empty (double-faced and other
multi-face layouts store their text per face), the **front face** from
`card_faces`. Text is matched case-insensitively with reminder text in
parentheses removed. Lands are never classified. Its rules are deliberately
conservative — a miss is preferred over a false positive:

| Role | Counts when the text … |
|---|---|
| Ramp | (a) on a **permanent** (not an instant or sorcery), contains an "add {…}" / "add one mana" / "add X mana"-style mana ability; or (b) "search(es) your library for … land card(s) … onto the battlefield"; or (c) puts a land card "from your hand onto the battlefield"; or (d) lets you "play an additional land"; or (e) creates a Treasure token |
| Card draw | contains "draw a card" / "draw N cards" (the imperative *you* draw), excluding "whenever you draw" / "when you draw" triggers |
| Targeted removal | "destroy / exile target …" or "return target … to its owner's hand" whose object is a creature, artifact, enchantment, planeswalker, battle, or permanent (not a *card* in a graveyard or library, and not a permanent "you control"); "deals N damage to any target / target creature / target planeswalker"; "target creature gets -N/-N" |
| Board wipe | "destroy / exile all …" or "destroy / exile each …" over creatures, artifacts, enchantments, planeswalkers, or (nonland) permanents; "return all … to their owners' hands"; "deals N damage to each creature"; "all creatures get -N/-N"; and any card with the Overload keyword that also qualifies as targeted removal |

**Known limits** (each a miss, not a false positive, unless stated):

- Counterspells are not removal; they belong to the separate Counterspell
  category.
- Edicts ("each opponent sacrifices"), fight / bite effects, tap-down and
  "can't attack" pseudo-removal, and -1/-1 counter placement are not removal.
- "Target player draws" and symmetrical "each player draws" draw, impulse draw
  ("exile the top card … you may play it"), card selection without draw
  (scry / surveil), and wheels are not card draw. A cantrip spell whose main job
  is something else still counts as draw — a mild overcount.
- Rituals (instants / sorceries that add mana) and cost reducers are not ramp;
  a planeswalker or creature with any mana ability is ramp even when that is a
  minor mode (e.g. Chandra, Torch of Defiance) — a mild overcount.
- One-sided wipes count as wipes; damage-based wipes that hit "each creature
  and each planeswalker" count; mass bounce of only your own permanents is not
  excluded.
- Only the front face of a multi-face card is read, so a modal double-faced
  card whose back is a land counts by its front face's text; the back face of a
  transforming card is ignored.
- Type rows inherit `deckstats`' primary-type reading, which looks at the whole
  type line: a modal double-faced "Instant // Land" counts as a land.

## `internal/bracket` — the bracket check

A pure package, sibling to `deckrules` and `deckstats`: `Check(input Input)
Report`, no DB and no AI. It estimates which Commander Bracket a deck plays at
and compares that with the bracket the owner chose (`decks.bracket`), so the
builder — and later the deck wizard — can keep a deck inside its bracket. The
handler assembles the input from already-loaded rows plus three lookups against
`card-data`'s mirror: `cards.is_game_changer`, the `combos` whose every card is
in the deck, and `spellbook_card_flags`.

### The rules it encodes

The official Wizards bracket rules (brackets are in beta) set **one hard number:
Game Changers** — 0 in Brackets 1–2, up to 3 in Bracket 3, unlimited in 4–5.
`cards.is_game_changer` (Scryfall's flag, refreshed daily) is the list; it is
never hard-coded. Everything else — two-card combos, mass land denial, extra
turns — Wizards now expresses as how many turns a game should last (B1 ≥ 9,
B2 ≥ 8, B3 ≥ 6, B4 ≥ 4), so the check surfaces those as **warnings the user
decides on**, never as blocks. Nothing in this package rejects a mutation; it
only reports.

The estimate is a Go port of Commander Spellbook's MIT-licensed
`estimate_bracket` classifier (`backend/spellbook/models/variant.py`,
attributed in the package doc), which tags a deck with one of Spellbook's
bracket tags. The port is faithful except where Spellbook's export lacks an
input (see *Decisions*).

**Input.** `Cards` — the deck's `main` + `command` cards, each with oracle id,
name, quantity, `GameChanger`, `MassLandDenial`, `ExtraTurn`; `Commanders` —
the oracle ids on the command board; `Combos` — candidate combos (every card
ingredient's oracle id is in the deck) with their ingredients, produced
features, `ManaValueNeeded`, and `HasNotablePrerequisites`; `TargetBracket`
(nil when unset).

**Matching.** A candidate combo counts only when the deck holds at least each
ingredient's quantity (main + command).

**Per-combo classification.**

- *relevant* — it produces a standalone (`S`) feature; *borderline relevant* —
  a standalone or contextual (`C`) one.
- *card count* — ingredients starting in the library are not counted;
  ingredients that are one of the deck's commanders are not counted (always
  available); every other ingredient counts its quantity as a *sure* card.
  Notable prerequisites add one *arguable* card, and so does not being
  borderline relevant. *Definitely two-card*: sure + arguable ≤ 2. *Arguably
  two-card*: sure ≤ 2 and sure + arguable ≤ 3.
- *speed* (5 fastest) from `ManaValueNeeded`: 0 → 5, ≤ 4 → 4, ≤ 6 → 3, ≤ 8 →
  2, otherwise 1.
- *mass land denial* — a feature named like "mass land destruction / denial /
  removal"; *extra turn* — a feature naming (near-)infinite (extra) turns, not
  for an opponent; *lock* — a feature name containing "lock"; *skip turns*,
  *control all opponents*, *control some opponents* — Spellbook's feature-name
  patterns.

**Deck tag** — the first rule that matches, in order:

| Tag | Spellbook name | When |
|---|---|---|
| `R` | Ruthless | > 3 Game Changers; ≥ 2 extra-turn cards; any mass-land-denial card; any combo with extra turns, mass land denial, or control of all opponents; any relevant, definitely-two-card combo of speed ≥ 4 |
| `S` | Spicy | any combo of speed ≥ 4 that is relevant and arguably two-card, or borderline relevant and definitely two-card; any lock, skip-turns, or control-some-opponents combo |
| `P` | Powerful | any relevant, definitely-two-card combo of speed ≥ 3; any Game Changer |
| `O` | Oddball | any borderline-relevant, arguably-two-card combo of speed ≥ 3 |
| `C` | Core | any extra-turn card; any relevant, definitely-two-card combo of speed ≥ 2 |
| `E` | Exhibition | otherwise |

Banned cards play no part: legality is `deckrules`' report, and an estimate is
still useful for a deck that currently holds a banned card.

**Tag → bracket.** Each tag is a *minimum*: `E` → 1, `C` → 2, `O` → 2
(borderline: may read as 3), `P` → 3, `S` → 3 (borderline: may read as 4),
`R` → 4. The estimate never says 5 — cEDH is a metagame, not a card list.

**Per-item tags.** Every matched combo is tagged by running the same cascade
over that combo alone; that tag and its minimum bracket are what the builder
shows beside the combo.

**Findings against the chosen bracket** (only when `TargetBracket` is set):

- *violation* — the Game Changer count exceeds the target's limit. The only
  hard rule; still reported, never enforced.
- *warning* — a combo whose minimum bracket is above the target; mass-land-
  denial cards in a deck targeting below 4; extra-turn cards in a deck
  targeting 1; two or more extra-turn cards in a deck targeting below 4.
- *notice* — a borderline combo (`O`, `S`) whose minimum bracket equals the
  target.

`Report` carries the deck tag, estimated bracket, and borderline flag; the
target; the Game Changer count, its limit (nil = unlimited, and nil while no
target is set), and their names; the mass-land-denial and extra-turn card
names; every matched combo (id, card names, produced standalone / contextual
feature names, mana needed, speed, the two-card and relevance flags, its tag
and minimum bracket); and the findings.

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
| `/api/decks/{id}/cards/{cardId}` | PATCH | `{ board, quantity? }` or `{ board, to_board }` | `204` / `400` / `404` | edits one existing entry in place: sets its quantity (`0` deletes it; a negative or out-of-range quantity is `400`), or moves it to `to_board` carrying its quantity / printing / category and merging into any entry already there. Ownership scoped in the query; `404` when no such entry (`DECK-012`, `DECK-013`) |
| `/api/decks/{id}/cards/{cardId}` | DELETE | — | `204` / `404` | scoped through `decks.user_id`; `204` only when a row was deleted, `404` when none matched |
| `/api/decks/{id}/validation` | GET | — | `ValidationReport` | |
| `/api/decks/{id}/stats` | GET | — | `Stats` + `category_targets` | deterministic; curve / pips / sources / category roll-up over `main` + `command` (`DECK-051`, `DECK-052`) |
| `/api/decks/{id}` | PATCH | `{ name?, description?, is_public?, bracket?, archetype? }` | `Deck` | `archetype` must be a known template key (else `400`, nothing stored); `""` clears it (`DECK-103`). `bracket` must be an integer 1–5, or `null` to clear it; anything else is `400` (`DECK-128`) |
| `/api/deck-templates` | GET | — | `{ version, templates }` | the authoritative template data, verbatim (`DECK-100`) |
| `/api/decks/{id}/template-fit` | GET | — | `{ archetype, template_version, bracket, rows }` | the deck's counts against its archetype's template; `archetype: null` and no rows when none is chosen (`DECK-104`) |
| `/api/decks/{id}/bracket` | GET | — | bracket `Report` + `combo_data_available` | deterministic (`internal/bracket`), over `main` + `command`; `combo_data_available` is false while the combo mirror is empty, so the builder can say combos were not checked (`DECK-126`) |
| `/public/decks/{id}` | GET | — | `DeckDetail` (read-only) | mounted at the router root, outside the `/api` auth group; unauthenticated; `404` (never `401`) unless `is_public` |

Adding a card **records the entry and returns it flagged** when the card is
outside colour identity — it does not silently reject or silently accept
(`DECK-004`). The full report is always available from `/validation`.

## Frontend

- **`/decks`** — the caller's decks as cards, plus a "New deck" action that
  `POST`s and routes to the builder.
- **`/decks/[id]`** — the builder, a multi-column workspace (`DECK-096`).
  `page.tsx` loads the deck + validation report, mounts the active-card
  provider, and arranges three columns above a narrow-viewport breakpoint,
  collapsing to one column below it:
  - **left column (sticky)** — the card-image panel (`DECK-070`) above the
    collapsible deck-stats and bracket sections (the legality summary is not in this column —
    it is pinned under the deck header, `DECK-096`);
  - **center column** — the type-grouped decklist;
  - **right column** — AI suggestions at the top (prominent — `ai-assist`), card
    search below.

  The deck header carries the deck name, colour identity, and a **Tools** menu
  whose only item opens the import/export panel in a dismissible dialog
  (`DECK-095`) — it is never inline. The slim legality summary is pinned
  (sticky) directly under the deck header, outside the three columns, so it
  stays visible while building at every breakpoint (`DECK-096`). The commander
  picker sits above the columns.
  - **Commander picker** — an autocomplete (`/api/cards/search?q=is:commander …`)
    over legendary creatures; selecting one `PUT`s `/commander`. The assigned
    commander / partner names and every result option drive the card-image
    panel on hover / focus.
  - **Card search** — a debounced box whose raw text goes straight to
    `/api/cards/search` (so `id:` / `t:` / `cmc` / `o:` / `is:commander` keep
    working), feeding a result list where each row shows name, mana cost (as
    pip chips), type line, and colour identity, the card's current total
    quantity in the deck when it is already present, and a transient
    confirmation on add. A **click anywhere on a row** — or Enter while the row
    holds the keyboard cursor — adds the card to `main` (`DECK-081`); a small
    secondary `⋯` control on the row (which a row click does not trigger) adds
    it to `sideboard` or `maybe` instead. The list is keyboard navigable —
    Arrow keys move a cursor, Home/End jump, Enter adds the row under the
    cursor.
  - **Decklist** — the board split (`command` shown as its own "Commander"
    section, then `main` / `maybe` / `sideboard`) is the outer level; within
    each board, entries are grouped by **primary card type** derived from
    `type_line` (`DECK-087`, `DECK-088`) with the summed count in each header
    ("Creatures (15)"). Each row is a card name that drives the image panel, its
    per-entry violation badges (never hidden), a `− n +` quantity stepper
    (decrement calls `PATCH …/cards/{cardId}` to set `quantity − 1`; the last
    decrement deletes the entry), and a `⋯` control that opens the action menu.
    `DecklistRow` exposes a `footer` slot; `Decklist` fills it on the main and
    command boards with the AI "Explain fit" control (see `ai-assist`), so a
    per-row AI affordance hangs off the row without the decklist depending on
    the AI feature itself.
  - **Card action menu** — opens **only** from the row's `⋯` control, never on
    hover (`DECK-090`); opening one row's menu closes any other (a single
    `openMenuEntryId` held by `Decklist`). It closes on outside click, Escape,
    a scroll of the decklist region, and navigation (`DECK-094`), and is
    `position: fixed` with a computed placement that opens below the row,
    flips above it near the viewport bottom, and clamps horizontally
    (`menuPlacement.ts`). It offers the context-valid actions (Add One, Add
    More, Remove One, Remove All, Move to Sideboard / Considering / Main, Copy
    Card Name), each showing its shortcut. Independently of the menu, while the
    pointer is over a row or the keyboard focus is within it and no field is
    focused, Alt+1 / Alt+2 / Alt+3 / Alt+4 fire Add One / Remove One / Move to
    Sideboard / Move to Considering on that row (`DECK-092`), matched on the
    physical digit key so macOS Option+digit characters do not interfere. The
    commander row's menu offers only Copy Card Name (`DECK-093`).
  - **Card-image panel** — a single sticky panel in the left column
    (`DECK-070`, Moxfield-style). Hovering or keyboard-focusing any card-name
    surface (`DECK-076`) sets it to that card; at rest it shows the deck's
    commander, or a neutral placeholder when there is none (`DECK-072`). It
    resolves `image_uris.normal` → `small` → a compact text card frame
    (`DECK-071`), decodes the image before swapping it in so it never flashes
    blank (`DECK-073`), and a short clear-delay hands the panel from one card
    directly to the next when the pointer crosses between adjacent surfaces
    (`DECK-074`). There is no floating tooltip and no portal (`DECK-078`); the
    panel lives inside the `.workspace` subtree and inherits the light palette
    directly.
  - **Legality summary** — a compact always-visible reading of the
    `/validation` report ("97/100", "2 cards outside colour identity",
    "singleton: 2× Sol Ring", "banned: Channel") pinned (sticky) directly under
    the deck header, visible at every breakpoint (`DECK-096`). Refetched after
    every mutation.
  - **Deck vs. target** — a left-column panel above the stats section
    (`DECK-109`). It carries an archetype picker (options from
    `/api/deck-templates`) and a bracket picker (1–5), both saved through
    `PATCH /api/decks/{id}`. With no archetype it prompts the player to pick
    one; with one it lists every row of `/template-fit` — lands, creatures,
    instants & sorceries, artifacts, enchantments, then ramp, card draw,
    targeted removal, board wipes — as "count / min–max", green when `ok` or
    `relaxed` (with a "Bracket 4" note naming the 32 floor on a relaxed land
    row), amber when
    `under` or `over`. Refetched when the deck's cards, commander, archetype,
    or bracket change.
  - **Deck stats** — a collapsible left-column section reading `/stats`: land /
    non-land / average MV, a bar-chart mana curve, colour pips vs sources, and
    category counts against the rules-of-thumb bands. Refetched when the deck's
    cards or commander change.
  - **Bracket** — a collapsible left-column section beside the stats, reading
    `/bracket` (`DECK-127`). Its header shows the estimate, the target, and the
    Game Changer count against the target's limit at a glance. Open, it has a
    target-bracket selector (1–5 or not set, `PATCH`ing `bracket`), the
    findings (violation / warning / notice tones), the Game Changer, mass
    land denial, and extra-turn card lists, and each matched combo — its
    cards, what it produces, its speed, its tag and minimum bracket — linking
    to the combo's Commander Spellbook page. It credits Commander Spellbook as
    the combo source, and says so plainly when combo data has not been loaded.
    Refetched when the deck's cards, commander, or target change.
  - **Import / export dialog** — see `import-export`; reached from the header
    Tools menu (`DECK-095`).

### Builder components and helpers

- `frontend/src/components/builder/` — `ActiveCardContext` (provider +
  `useActiveCard` state of which card the image panel shows), `CardImagePanel`
  (the sticky panel), `CardFrame` (the shared text-frame fallback),
  `HoverCardName` / `useActiveCardTrigger` (the per-surface hover/focus
  trigger), `CommanderPicker`, `CardSearch` / `SearchResultList` /
  `SearchResultRow`, `ManaSymbols` (`ManaCost` / `ColorIdentity` chips),
  `Decklist` / `DecklistRow` / `QuantityStepper`, `CardActionMenu`,
  `ToolsMenu` + `ImportExportDialog`, `BracketPanel` (the bracket section,
  `DECK-127`). `Decklist` owns the single
  `openMenuEntryId` and also renders the AI "Explain fit" control
  (`explainFitLabel` from `lib/deck.ts`) through `DecklistRow`'s `footer` slot
  on the main / command boards — see `ai-assist`. Each is `"use client"`; the
  whole builder renders inside the `(app)` layout's `.workspace` wrapper, so
  every panel already carries the light palette (`PLATFORM-023`) with no portal.
- `frontend/src/lib/cardPreview.ts` — pure: `resolvePreviewImage`
  (normal → small → text frame) and the card-image aspect dimensions.
- `frontend/src/lib/menuPlacement.ts` — pure: `computeMenuPlacement` (open
  below the anchor, flip above near the viewport bottom, clamp horizontally).
- `frontend/src/lib/searchNav.ts` — pure: `moveCursor` (keyboard cursor over
  the result list), `deckCardQuantity` (a card's total across all boards).
- `frontend/src/lib/cardActions.ts` — pure: `buildCardActions` (the ordered,
  context-filtered action list, with the deck API passed in), `findShortcutAction`.
- `frontend/src/lib/deck.ts` — pure helpers: `groupByType` (primary-type
  buckets in display order with counts, `DECK-087` / `DECK-088`), `boardCount`,
  `formatValidationStrip(report)`, `primaryCardType` / `activeShortcutRow`
  (physical-key chord owner: pointer row, else focus-within row, `DECK-092`),
  `resolvePanelCard` (active card → commander → placeholder resolution for the
  image panel, `DECK-072`).
- `frontend/src/lib/deckstats.ts` — pure view helpers over the stats payload:
  `curveRows`, `pipRows`, `categoryRows`.
- `frontend/src/lib/templateFit.ts` — pure view helpers over the template-fit
  payload: `fitTone` (status → green / amber) and `formatFitRow`.
- `frontend/src/components/builder/TemplateFitPanel.tsx` — the "deck vs.
  target" panel.
- `frontend/src/lib/bracket.ts` — pure view helpers over the bracket payload:
  `bracketHeadline` (estimate / target / Game Changer budget), `tagLabel`,
  `findingTone`, `comboUrl`, `parseBracketChoice`.

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
| Editing an existing entry | One `PATCH …/cards/{cardId}` that both sets an absolute quantity (`0` = delete) and moves an entry between boards (`to_board`) | A `delta`-based increment/decrement endpoint; separate `/quantity` and `/move` endpoints; client-side delete-then-re-add loop for a decrement | The builder needs per-copy quantity edits (basic lands) and board moves (the action menu), and `POST` (only `+1`) plus `DELETE` (whole entry) cannot express either without N round-trips. An absolute `quantity` is idempotent and needs no read-modify-write. The move is the same "edit one entry" shape, so it rides the same endpoint rather than doubling the surface; it is one statement (delete-returning + insert-on-conflict) so the quantity merges if the target board already holds the card. |
| Card preview delivery | A fixed, sticky image panel in the builder's left column; every card-name surface is a `HoverCardName` that sets shared "active card" state on hover / focus, and the panel shows the active card, else the commander, else a placeholder | The earlier floating tooltip portalled into `document.body`; a popover per row | The captain ran the floating version and found it unusable — sweeping or scrolling the lists left tooltips stranded over the content. A single panel that never overlaps anything, is always in the same place, and stays put while lists scroll is the Moxfield pattern and removes the portal, the placement math, and the scroll/Escape dismissal entirely. It also needs no `.workspace` re-scoping because it renders inside the builder subtree. |
| Action-menu open trigger | Open **only** from the row's `⋯` control; `Decklist` holds one `openMenuEntryId` so opening one closes any other; close on outside click / Escape / decklist scroll / navigation | Open on hover after a delay (the earlier behaviour); allow several open at once | Hover-open stranded menus over the list on scroll exactly as the tooltip did, and several could stack. A click trigger with a single-open invariant and scroll-close is unambiguous and cannot litter the list. |
| Action-menu shortcut binding | Bind Alt+1..4 to exactly one row — the row under the pointer, else the row with keyboard focus-within, pointer winning when both are set — resolved by `Decklist` (`activeShortcutRow`) so only that one row's effect listens; the effect does not depend on the menu being rendered; match on `event.code` (`Digit1`..`Digit4`); guard against a focused field | Bind on every row that is either hovered or focus-within (fires the chord twice — once per matching row — when the pointer and the keyboard focus are on different rows); bind only while the menu is open; match on `event.key`; a global shortcut layer | The chords must keep working now that the menu no longer opens on hover, so they cannot hang off the menu. Binding on hover-or-focus independently let a chord fire on two rows at once (pointer over row A, focus in row B), issuing a duplicate PATCH — resolving a single owner id in the parent, pointer first, removes that. `event.code` is the physical key, sidestepping macOS Option+digit typography, and the field guard stops the chords firing mid-search. |
| Add-from-search interaction | A click anywhere on the result row (and Enter on the cursor row) adds to `main`; a secondary `⋯` on the row adds to `sideboard` / `maybe` | Keep the dedicated "Add" button as the primary action | The captain asked for "click the name = add". The overwhelmingly common action is "add this to my deck"; making the whole row that action removes a target the user otherwise has to aim for, and the rare other-board add stays available behind a control that a row click does not reach. |
| Decklist grouping | Group each board's entries by primary card type derived from `type_line` (front face of a `//` line; precedence Creature → Planeswalker → Land → Artifact → Enchantment → Instant → Sorcery → Battle → Other), board split as the outer level, commander as its own section | Keep grouping by the free-text `category` field; group by type only within `main` | `category` is hand-entered and empty for most entries, so it collapsed to one "Uncategorised" list — the captain asked for the Moxfield type sections with counts. Type is always derivable. Precedence mirrors `internal/deckstats` so the decklist sections and the stats type counts agree. The free-text category grouping returns with the M5 auto-categorizer. |
| Template definition home | One versioned Go package (`internal/decktemplates`) served verbatim at `GET /api/deck-templates`; the fit is evaluated server-side | Ranges in a frontend constant; a `deck_templates` DB table; extending `deckstats.CategoryTargets` | The later wizard must read the same templates the panel shows, so there is exactly one copy and every client fetches it. Code (not a table) keeps the ranges reviewed and versioned alongside the evaluation that reads them; a table would need an admin path nobody has. `CategoryTargets` is a single archetype-free band set over hand-tagged categories, a different thing. |
| Template type counting | Reuse `deckstats.Analyze`'s primary-type counts over the `main` board | A dedicated "counts as artifact *and* creature" multi-type count | One precedence everywhere (decklist sections, stats, templates) means the numbers a player sees always agree; an artifact creature fills a creature slot in practice. |
| Template role counting | A `deckroles.Classifier` interface with a conservative, deterministic Oracle-text default | Count the free-text `category` field; ingest Scryfall `oracle_tags` now; ask the LLM | `category` is empty for most entries, so it would read "under" everywhere. Tag ingestion is `CARD-010` and its own piece of work; the interface lets it drop in later. An LLM call per panel refresh is slow, costly, and nondeterministic. Conservative rules mean a deck reads slightly "under" rather than falsely "ok". |
| Archetype storage | Nullable `decks.archetype` text holding a template key, validated in the handler | A Postgres enum or CHECK listing the keys; a separate `deck_templates` FK | Templates live in code; a DB-side key list would be a second copy needing a migration per new template. |
| Bracket 4 land relaxation | `relaxed_at_bracket: 4` + `relaxed_floor: 32` on the land row in the template data, keyed on the deck's self-assigned `bracket`, Bracket 4 only | Relax at Bracket 4 and above; no floor; detect fast mana from the decklist now; widen every land range | The captain's call: the relaxation is for a Bracket 4 deck with heavy fast mana, with 32 as the floor. Bracket 5 decks are not assumed to run lean. Without a floor a 0-land deck would read green. Fast-mana counting needs role tags (`CARD-010`) or the M6 Game Changer work, so the bracket claim stands in until then; the data shape stays the same when the check is added. |
| Bracket estimate | A pure Go port of Commander Spellbook's MIT `estimate_bracket` classifier over the local combo mirror | Call Spellbook's `POST /estimate-bracket` per request; ask the language model to judge the bracket | The live call is an external dependency on every check and every future wizard turn; a model judgement is not deterministic and breaks *statistics and rules decide*. Porting keeps one well-tested community rule set, runnable offline and in tests. |
| What the bracket check enforces | Report only: Game Changers over the limit are a *violation*, combos / MLD / extra turns are *warnings* or *notices*; no mutation is ever rejected | Reject adds that break the target bracket | Wizards' only hard number is the Game Changer count; the rest is now phrased as turn expectations the pod agrees on. Players add cards mid-build that they will cut later, exactly like the colour-identity flag (`DECK-004`). |
| Mana-figure accuracy in the speed heuristic | Treat every combo's mana figure as an accurate minimum | Assume all are inaccurate (+1 speed) | Spellbook's live classifier adds one speed step when an editor marked the figure inaccurate, but its export does not carry that flag. Accurate is Spellbook's own default; the cost is that a few combos read one speed step slower than Spellbook's live estimate. |
| Banned cards in the estimate | Ignored by the estimate (Spellbook tags such a deck `B`) | Report no estimate while a banned card is present | Legality is `deckrules`' job and already shown; an estimate stays useful for a deck that is mid-fix. |
| Bracket 5 | Never estimated; a target of 5 produces no findings | Treat 4+ decks as 5 | cEDH is defined by its metagame, not by detectable cards; Game Changers are unlimited there as in 4. |
| Import/export placement | Behind a "Tools" menu on the deck header, opening a dismissible dialog | Keep it as an always-on inline panel | It is used once or twice per session; an always-on panel was pure vertical bloat on a page the captain already found too long. |

## Open Questions & Future Decisions

### Deferred
1. **Deck-bootstrapping wizard** (captain bonus #1) — reads its slot targets
   from `internal/decktemplates` (the same data as the "deck vs. target"
   panel) once the player picks an archetype. Given a commander,
   auto-populate a mana base from the colour identity, then walk a wizard adding
   the most powerful/common cards for those colours. Needs: a "generate mana
   base" endpoint (basics + staple lands by `edhrec_rank` within identity), a
   wizard-state model (which slots are filled), and a hand-off to `ai-assist`
   for the "top cards by colour" step. The `deck_cards` model already supports
   it — bulk inserts with `category = 'Land'`. Related to roadmap M8.
2. **Functional-subtype grouping as first-class** (captain bonus #4) — the
   decklist now groups by primary card *type* (`DECK-087`), which is always
   derivable. Grouping by *functional category* (Removal / Counterspell / Ramp /
   Card Draw / Board Wipe / …) still waits on the full auto-categorizer
   (Oracle-text heuristics + Scryfall `oracle_tags`, roadmap M5, depends on
   `CARD-010`), since the hand-entered `category` field is empty for most
   entries. `deckstats` already rolls category counts up against the Commander
   rules-of-thumb bands; the builder shows those in the stats section.
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
11. **Double-faced card preview flip** (`DECK-077`) — the card-image panel shows
    a DFC's front face only. `CardSummary` / `DeckEntry` expose a single
    `image_uris` object, so a per-face flip control needs `card-data` to surface
    both faces first.
12. **Tap-to-load on coarse pointers** — a touch user has no hover, so the
    card-image panel updates only on keyboard focus of a card name (`DECK-075`)
    and otherwise shows the commander or the placeholder. A deliberate tap
    affordance that loads the tapped card name into the panel is not built in
    v1.
13. **Out-of-scope action-menu items** — Moxfield's menu also carries printing /
    foil / tag / deck-image / collection actions. They are omitted here until
    the features behind them exist (printing selection, tags, collections).

14. **Fast-mana-aware land relaxation** — the Bracket 4 land relaxation
    trusts the self-assigned bracket. Requiring the deck to actually run heavy
    fast mana waits on Scryfall role tags (`CARD-010`) or M6's fast-mana /
    Game Changer counting.
15. **Tag-backed role classifier** — a `deckroles.Classifier` over Scryfall
    `oracle_tags` (`CARD-010`) to replace the Oracle-text rules and lift their
    known limits.

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
  `backend/internal/decktemplates/`, `backend/internal/deckroles/`,
  `backend/internal/bracket/`, `backend/internal/api/decks.go`,
  `backend/internal/api/stats.go`, `backend/internal/api/templates.go`,
  `backend/internal/api/bracket.go`,
  `backend/internal/db/migrations/000006_add_deck_archetype.up.sql`,
  `backend/internal/db/migrations/000003_create_decks.up.sql`,
  `backend/internal/db/queries/decks.sql`
- Tests: `backend/internal/deckrules/deckrules_test.go`,
  `backend/internal/deckstats/deckstats_test.go`,
  `backend/internal/decktemplates/decktemplates_test.go`,
  `backend/internal/deckroles/deckroles_test.go`,
  `backend/internal/bracket/bracket_test.go`,
  `backend/internal/api/templates_test.go`,
  `backend/internal/api/decks_test.go`, `backend/internal/api/stats_test.go`,
  `backend/internal/api/bracket_test.go`
- Frontend: `frontend/src/app/(app)/decks/page.tsx`,
  `frontend/src/app/(app)/decks/[id]/page.tsx`,
  `frontend/src/components/builder/` (active-card provider, sticky card-image
  panel + shared text frame, hover/focus trigger, commander picker, card search
  + result rows, mana chips, decklist + row + quantity stepper, card action
  menu, Tools menu + import/export dialog),
  `frontend/src/lib/cardPreview.ts`, `frontend/src/lib/menuPlacement.ts`,
  `frontend/src/lib/searchNav.ts`, `frontend/src/lib/cardActions.ts`,
  `frontend/src/lib/deck.ts`, `frontend/src/lib/deckstats.ts`,
  `frontend/src/lib/templateFit.ts`, `frontend/src/lib/bracket.ts`
- Frontend tests: `frontend/src/lib/templateFit.test.ts`, `frontend/src/lib/cardPreview.test.ts`,
  `frontend/src/lib/menuPlacement.test.ts`, `frontend/src/lib/deck.test.ts`,
  `frontend/src/lib/searchNav.test.ts`, `frontend/src/lib/cardActions.test.ts`,
  `frontend/src/lib/bracket.test.ts`
- Cross-segment: reads `card-data` (`cards`, `card_prints`, `banlist_overrides`,
  and the Commander Spellbook mirror `combos` / `combo_cards` /
  `spellbook_card_flags`);
  `user_id` / `anon_token` ownership comes from `account-access` (M1: the
  `DevAuth` user). `ai-assist` reuses `internal/deckrules` to gate model output.
  `import-export` writes `deck_cards` in bulk from a parsed decklist.
