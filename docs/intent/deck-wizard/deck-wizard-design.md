---
parent: high-level-design
prefix: WIZ
---

# Deck Wizard

The deck wizard is an interactive scaffolder. A player names a commander, a
Commander Bracket, and any restrictions, and the wizard walks them through the
build one question at a time: which kinds of lands they will play, which utility
lands they want, which ramp, card draw, removal and board wipes the archetype
calls for, and then a stream of synergy picks they accept or reject, all inside
the bracket's Game Changer and combo limits. **It never generates a
whole deck.** Every card enters the deck only because the player clicked
Accept, through the same add-card endpoint the builder already uses; the wizard's
backend proposes and never writes a deck.

The wizard is the first consumer of four pieces built before it: the bracket
check (`deck-building`, `DECK-120`..`DECK-128`), the archetype templates
(`DECK-100`..`DECK-109`), Scryfall oracle tags (`card-data`, `CARD-060`..`067`),
and the on-demand Archidekt synergy data (`synergy`). It adds no data source.

## Context and Design Philosophy

The governing HLD tenet is *the model curates and explains; statistics and rules
decide*. For the wizard that splits the work cleanly:

- **Deterministic, in `internal/deckwizard` and SQL.** What may be proposed at
  all: the candidate pool for each step, the price and tutor restrictions, the
  bracket's Game Changer limit and combo / mass-land-denial / extra-turn
  expectations, the land target, and the basic-land split. A rule that must hold
  is a filter on the pool, never a sentence in a prompt.
- **The model, one call per step.** From a bounded pool it picks the best few for
  this commander, strategy and bracket and says why in a sentence. It cannot add
  a card the pool lacks.
- **The gate, unchanged.** Whatever the model names passes the same
  validate-every-returned-card gate as "suggest & explain" (`AI-010`): resolve in
  the mirror, re-validate with `internal/deckrules`, drop duplicates and cards
  already in the deck. The wizard then also requires pool membership and
  re-applies the bracket check to the survivors.

Because the pool is already legal, in-bracket and within the player's
restrictions, the failure modes are small: a model that returns nonsense yields
fewer proposals, never an illegal or out-of-bracket one.

The wizard is a **signed-in feature** like every AI endpoint (`AI-033`). Its
deterministic parts (the plan and the basic-land split) need neither a model nor
a sign-in.

## The Flow (the first slice)

The wizard is the smallest end-to-end path that works. Six steps, in
order; the player can stop after any of them and carry on in the builder.

| # | Step | Player is asked | Candidate pool (deterministic) | Model's job | Who writes the deck |
|---|---|---|---|---|---|
| 0 | **Setup** | commander (already on the deck), bracket 1–4, optional archetype, restrictions (no tutors, price cap per card, free-text strategy notes) | — | none | the existing `PATCH /api/decks/{id}` for bracket and archetype |
| 1 | **Fixing lands** | "Basics only, or include dual / fixing lands?" and "fetch lands too?" | nonbasic lands that make two or more of the deck's colours, plus fetch lands if allowed | pick up to 10 and explain | Accept → existing add-card |
| 2 | **Utility lands** | "Any utility lands (man-lands, colourless utility)?" | lands tagged `utility-land` (or a descendant) | pick up to 5 and explain | Accept → existing add-card |
| 3 | **Roles** | for each of ramp, card draw, targeted removal and board wipes: how many the deck has against the archetype's target range, and "suggest some?" | nonland cards the role classifier assigns that role, synergy-sample cards first, then `edhrec_rank` | pick up to 8 (never more than the room left under the range's maximum) and explain | Accept → existing add-card |
| 4 | **Picks** | nothing: the player reviews rounds of up to 10 proposals and accepts or rejects each; "more" asks for another round | synergy-ranked cards (Archidekt), or `edhrec_rank`-ranked while synergy is gathering, plus the Game Changers the bracket still allows | pick up to 10 and explain, steered by the strategy notes | Accept → existing add-card |
| 5 | **Basics** | "Fill the rest with basics?" | the remaining land slots, split by colour-pip demand | none (deterministic) | the player's click → existing add-card |

**Dual-land avoidance** is a choice, not a filter on the model: a player who
picks "basics only" makes no step-1 call at all, and no later step ever proposes
a land — the picks pool is nonland cards only (`WIZ-033`) — so a player who does
not want duals never sees one. The price cap addresses the other common worry:
the player who is fine with duals but not with expensive ones.

**Interactivity** is structural. A proposal list changes the deck only through
the player's Accept click; a Reject adds the card to the deck's rejected set
that later rounds exclude. There is no "build it for me" path in the API or the
UI.

**Deferred past this slice** (listed under *Open Questions & Future Decisions*):
Bracket 5, partner commanders, a fast-mana-specific pass
for Bracket 4, and a free-form chat with the model.

## Role Steps

The roles step fills the archetype template's four functional rows (`ramp`,
`card_draw`, `targeted_removal`, `board_wipe`) with cards the player accepts. It
adds no classifier and no template data: counting and candidate selection both
use `internal/deckroles` (the `OracleClassifier` behind `roleClassifier`, the same
instance the deck-vs-target panel uses) and the ranges come from
`internal/decktemplates`, so the number the wizard shows is the number the
builder shows.

- **Where the player stands.** The plan (`GET …/wizard`) carries a `roles` list:
  per role, the count on `main` (by quantity, the commander excluded, as the
  template-fit endpoint counts), the template's `min` / `max`, and the
  `decktemplates.Evaluate` status (`ok` / `under` / `over`). A deck with no
  archetype has no template, so the list is empty and the step asks the player to
  choose an archetype in setup.
- **One role per round.** `POST …/wizard/propose` with `step: "roles"` and
  `role: "ramp" | "card_draw" | "targeted_removal" | "board_wipe"`. The panel's
  roles step shows the four rows with their counts and lets the player ask for
  any one of them, so they choose which gaps to fill and in what order.
- **Room, not just need.** A round asks for `min(8, max − count)` cards. At or
  past the range's maximum the answer is empty (`role_filled`) with no model call:
  the template says the deck has enough of that role. Below the minimum or inside
  the range the player may still ask, up to the maximum.
- **The pool.** Nonland cards inside the colour identity and commander-legal,
  not on the deck and not rejected, that the classifier assigns the role, up to 30.
  Order: cards from the commander's stored synergy sample first (in its order),
  then `edhrec_rank`. A SQL regex prefilter narrows the mirror to cards whose Oracle
  text could plausibly carry the role (ramp: mana abilities, land search, Treasure;
  draw: "draw"; and so on) so the Go classifier sees a few hundred rows, not the
  whole mirror; the classifier alone decides membership, so a loose prefilter costs
  rows, never correctness. Every other pool rule applies unchanged: the price cap,
  the tutor ban, bracket admission on the pool and on the survivors, pool
  membership and the validate-every-returned-card gate. A role card that is a Game
  Changer competes under the same budget as in picks and is flagged.
- **The model's job.** The step guidance names the role, the deck's count and the
  target range, and asks for the best fits for this commander and strategy,
  favouring cheap and efficient cards and mixing sub-types (for removal: spot
  removal against flexible answers; for ramp: rocks against land-fetching).
- **Roles are not a classifier replacement.** Because the Oracle-text classifier
  prefers a miss to a false positive, a card the classifier does not recognise is
  never offered by this step; the picks step still reaches it.

## Where It Lives in the Builder

The wizard is a panel in the builder's right column (`DECK-096`), not a separate
page. While it is open it **replaces the "AI suggestions" panel** (it is the
guided form of the same idea) and card search stays below it. The decklist in
the centre column and the image panel, "deck vs. target", stats and bracket
sections in the left column stay live, so each Accept is visible at once and the
player can hover any proposal for its card image (`DECK-076`).

It opens from a **Build with wizard** button in the deck header, or on load when
the URL carries `?wizard=1`. The decks list offers **New deck with wizard**, which
creates a deck and routes to `/decks/{id}?wizard=1`. Closing the panel returns
the suggestions panel; nothing is lost, because every accepted card is already
in the deck.

Wizard answers (restrictions, land preferences, the rejected set and the current
step) are saved per deck in `deck_wizard_state` (see *Persisted Answers*), so a
reload resumes where the player left off. The deck itself still holds the
commander, bracket, archetype and every accepted card; the saved state holds only
what the wizard asked.

## Persisted Answers

Each deck has at most one `deck_wizard_state` row (`deck_id` primary key,
`ON DELETE CASCADE`, `state jsonb`, `updated_at`), written only through the
owner-gated state endpoints. The state is the wizard's answers and nothing the
deck already holds:

```json
{ "step": "utility",                  // setup | fixing | utility | picks | basics
  "restrictions": { "no_tutors": true, "max_price_usd": 5, "notes": "tokens" },
  "fixing": "include",                // include | basics
  "include_fetch": true,
  "utility": true,
  "land_target": null,                // 20-60, or null for the plan's default
  "rejected_card_ids": ["…"] }        // at most 200
```

The server validates the whole body with the same rules as a propose request
(`WIZ-012`) and stores it as given; it does not interpret it. A save replaces the
row. Proposals themselves are not saved: a reload re-asks the step, which is one
metered call the player chooses to make, and the saved rejected set keeps already
decided cards out of it.

The panel reads the state when it opens and shows nothing until the read has
finished, so it can never save defaults over an earlier session. After that it
saves on every change to the answers, the rejected set or the step, after a short
debounce. A saved step that the deck cannot show now (the fixing step for a deck
that has become mono-coloured, or any step past setup for a deck that lost its
commander or bracket) opens at setup instead. **Start over** deletes the row and
resets the panel; the deck is untouched. A failed read or save is reported in the
panel and does not block it: the answers stay in page state, and after a failed
read the panel saves nothing, so defaults never overwrite a state it could not
see.

Ownership is the deck's (`deckForOwner`), so anonymous-draft owners persist their
answers too, and claiming a draft on sign-in carries the deck, and so its state,
with it. State is not exposed on the public share view.

## Bracket Policy (`internal/deckwizard`)

`PolicyFor(bracket)` returns, for brackets 1–4, the Game Changer limit
(`bracket.GameChangerLimit`: 0, 0, 3, unlimited) and a short guidance text for
the model. Bracket 5 and anything else is not supported: the plan reports
`bracket_unsupported` and a propose request is `422`.

| Bracket | Game Changers | Guidance given to the model (steering only; the filters below are binding) |
|---|---|---|
| 1 Exhibition | none | theme over power; no combos, no extra turns, no mass land denial |
| 2 Core | none | precon-level power; no infinite combos; no mass land denial; extra turns incidental |
| 3 Upgraded | up to 3, chosen sparingly | some combos, none that win quickly; no mass land denial |
| 4 Optimized | unlimited | go for efficient interaction, fast mana, Game Changers and strong combos |

### Admission: what the bracket check lets through

`Admit` decides whether a candidate card may be proposed, by running
`internal/bracket.Check` over the deck with and without the candidate and the
combos of the local Spellbook mirror:

1. a Game Changer is admissible only while the deck's Game Changer count plus the
   candidate stays within the bracket's limit (an explicit count check, so a deck
   already over the limit admits no further Game Changer);
2. any other candidate is admissible unless adding it produces a **warning** the
   deck did not already have: a combo whose minimum bracket exceeds the target,
   mass land denial below Bracket 4, an extra-turn card in Bracket 1, or two or
   more extra-turn cards below Bracket 4. A *notice* (a borderline combo at
   exactly the target bracket) is allowed.

It is applied twice: **to the pool**, each candidate alone against the deck, so the
model never sees an inadmissible card; and **to the gated survivors**, one after
another in the model's order with each admitted card joining the working deck, so
that accepting any subset of one response keeps the deck inside the bracket even
when two proposals only conflict with each other. When the combo mirror is empty,
combos are not checked, the response says `combos_checked: false`, and the Game
Changer limit still holds.

Admission shapes what the wizard proposes; it does not constrain the deck. The
builder's add endpoint still accepts anything the player adds by hand, and the
Bracket panel remains the report (`DECK-125`).

## Pools

All pools come from the mirror and share one legality definition with the rest of
the product: within the deck's colour identity, commander-legal after
`banlist_overrides`, not already on any board of the deck, not in the player's
rejected set. Every pool is then narrowed by the **restrictions**:

- `no_tutors` removes cards carrying the `tutor` oracle tag or a descendant
  (`FilterCardIDsByOracleTag`).
- `max_price_usd` removes cards whose **price** exceeds the cap. A card's price is
  the lowest `prices.usd` among its non-digital printings; a card with no known
  price is removed when a cap is set.
- `notes` (at most 500 characters) is passed to the model as the player's own
  untrusted preference text. It steers curation. It cannot widen a pool or relax
  a rule, and the model's answer is gated like any other.

| Step | Source | Ordering | Size |
|---|---|---|---|
| fixing | nonbasic lands (front face a Land) with at least two of the deck's colours in `produced_mana`, plus, when `include_fetch`, lands that sacrifice themselves to search the library (by Oracle text) or carry the `fetchland` tag | `edhrec_rank`, unranked last | 30 |
| utility | lands tagged `utility-land` or a descendant | `edhrec_rank`, unranked last | 25 |
| roles | nonland cards the role classifier assigns the requested role: the commander's stored synergy cards first, then by `edhrec_rank` (see *Role Steps*) | synergy order, then `edhrec_rank` | 30 |
| picks | the commander's stored synergy cards when a sample exists, else nonland cards by `edhrec_rank`; plus up to 10 identity-legal Game Changers when the bracket still allows them | synergy descending (then inclusion) or `edhrec_rank` | 40 |

The picks pool never contains a land, whatever the source (`WIZ-033`). Land
steps ask for at most as many cards as the land target leaves room for; when the
deck already meets the target, or the identity has fewer than two colours (nothing
to fix), the step answers empty without a model call.

## Land Target and Basics

The **land target** is the midpoint (rounded down) of the chosen archetype's land
range from `internal/decktemplates`, or 37 with no archetype. The Bracket 4
relaxation is not applied: the wizard cannot know yet whether the deck will run
heavy fast mana, so the player may lower the target by hand. The player can
override the target in the panel (20–60).

The **basics plan** is `needed = max(0, target − lands already on main + command)`,
split across the identity's colours by largest-remainder allocation over weights
`max(pips, 1)`, where `pips` is `deckstats`' coloured-pip demand across the whole
deck including the commander; ties break in W-U-B-R-G order. A colourless identity
uses Wastes. The nonbasic lands' sources are not netted out of the split. The
endpoint returns the plan; the client applies it on a click, so repeating the step
after the deck changes simply recomputes.

## Model Curation (`ai.Assistant.Curate`)

One new `Assistant` method, `Curate`, serves every step. The request carries the
commander (name, Oracle text), the deck's colour identity, the bracket and its
guidance, the Game Changer budget left, the step's guidance text, the player's
notes, the names already in the deck, the pool (each entry: name, cost, type,
Oracle text trimmed to 300 characters, produced mana, price, whether it is a Game
Changer, and the synergy inclusion and synergy points when present) and `Want`.
The model answers through a forced `submit_picks` tool call: a two-sentence
`summary` addressed to the player and up to `Want` `{name, reason}` picks. A single
`Messages.New` call, no search tool: the pool is the whole menu. It uses the same
model as suggest (`AI-004`). Token usage is returned on every path so the handler
meters a call that reached the model whatever its outcome (`AI-034`).

An answer with no `submit_picks` call is an error (`502`); an answer that picks
nothing is a valid, empty result.

## Spend

Each propose request that reaches the model is **one call** against the new
`wizard` feature in `ai_usage` (migration `add_wizard_ai_feature` widens the
table's `feature` check to admit it). It shares everything else with the
existing AI features:

- a per-user daily limit, `AI_WIZARD_DAILY_LIMIT` (default 40), enforced exactly
  like `AI_SUGGEST_DAILY_LIMIT` (`429`);
- the one global monthly spend ceiling `AI_MONTHLY_SPEND_USD` (`503`), which sums
  every feature, so the wizard cannot spend past it;
- metering of every call that returned tokens, before any error mapping
  (`AI-030`, `AI-034`);
- the same gate order — disabled → anonymous → not the owner → ceiling → daily
  limit — so ownership is checked first (`AI-035`).

A full run is about twelve calls: at most one each for fixing and utility, one per
role the player asks for (up to four), then a round per ten picks. Choosing "basics only", skipping utility lands, a mono-colour
deck, a met land target, a role already at its maximum, a deck with no archetype,
a full deck, an empty pool, the plan and the basics split all cost no model call.

## HTTP API

All under the protected `/api` group, registered by `RegisterWizardRoutes`.

| Endpoint | Method | Gates | Purpose |
|---|---|---|---|
| `/api/decks/{id}/wizard` | GET | ownership (`404`) | the plan: readiness, bracket policy and Game Changer budget, land target and counts, synergy status |
| `/api/decks/{id}/wizard/propose` | POST | AI gates (`503`/`403`/`404`/`503`/`429`) | one curated round for a step (`fixing`, `utility`, `roles` with a `role`, `picks`) |
| `/api/decks/{id}/wizard/basics` | POST | ownership (`404`) | the basic-land plan; writes nothing |
| `/api/decks/{id}/wizard/state` | GET | ownership (`404`) | the saved answers, or `saved: false` |
| `/api/decks/{id}/wizard/state` | PUT | ownership (`404`), validation (`400`) | replace the saved answers |
| `/api/decks/{id}/wizard/state` | DELETE | ownership (`404`) | forget the saved answers (`204`) |

```json
// GET → WizardPlan
{ "ready": true, "reason": null, "ai_enabled": true,
  "bracket": 3, "archetype": "midrange_value",
  "policy": { "game_changer_limit": 3, "game_changers_in_deck": 1,
              "game_changers_left": 2, "guidance": "…" },
  "lands": { "target": 37, "in_deck": 12, "basics_needed": 25,
             "fixing_applicable": true },
  "roles": [ { "role": "ramp", "label": "Ramp", "count": 4, "min": 10, "max": 12,
               "status": "under" } ],          // [] without an archetype
  "synergy": { "status": "gathering", "reason": null } }
// reason: "no_commander" | "no_bracket" | "bracket_unsupported" when not ready

// POST propose → body
{ "step": "picks",                      // "fixing" | "utility" | "roles" | "picks"
  "role": "ramp",                       // roles only: ramp | card_draw | targeted_removal | board_wipe
  "include_fetch": true,                // fixing only
  "land_target": 37,                    // optional, 20–60
  "restrictions": { "no_tutors": false, "max_price_usd": 5, "notes": "tokens" },
  "rejected_card_ids": ["…"] }          // at most 200

// POST propose → response
{ "step": "picks", "summary": "…", "source": "synergy",   // "synergy" | "popular" | "lands"
  "proposals": [ { "card": CardSummary, "reason": "…", "game_changer": false,
                   "price_usd": 1.2,
                   "synergy": { "decks": 36, "inclusion": 0.95, "synergy": 0.87 } } ],
  "dropped": 1, "pool_size": 31, "empty_reason": null,
  "game_changers": { "limit": 3, "in_deck": 1, "left": 2 },
  "combos_checked": true,
  "synergy": { "status": "ready", "sample": { … }, "attribution": { "text": "…", "url": "…" } },
  "model": "claude-sonnet-5" }
// empty_reason: "land_target_met" | "no_fixing_needed" | "deck_full" | "no_candidates"
//               | "no_archetype" | "role_filled"

// POST basics → { "land_target": 37 } → response
{ "land_target": 37, "lands_in_deck": 12, "basics_needed": 25,
  "rows": [ { "card": CardSummary, "quantity": 10, "in_deck": 0 } ], "missing": [] }
```

`proposals[].synergy` and the response's `synergy` block are `null` unless the
numbers came from stored synergy data; when present they carry the Archidekt
attribution and the sample's deck count (`SYN-040`) so the panel can show the
required line (`SYN-043`). The plan calls `Ensure` for the commander when it is
ready and a synergy service is configured, so a wizard opened on a fresh
commander starts the gathering the picks step will use.

## Frontend

- `frontend/src/components/builder/wizard/WizardPanel.tsx` — the panel, with the
  six steps (setup, fixing lands, utility lands, roles, picks, basics) and the
  round result shared by the curated steps; `ProposalList.tsx` — one round's
  cards with Accept and Reject.
- `frontend/src/lib/wizard.ts` — pure, tested: step order and skipping rules,
  the rejected-set reducer, request-body builders, the role rows' wording, the Game Changer budget line,
  empty-reason and error-status messages, and the basics apply plan.
- `frontend/src/lib/api.ts` — types and `getWizardPlan`, `wizardPropose`,
  `wizardBasics`; `addCard` gains no new behaviour (the basics step sets a row's
  quantity with `setCardQuantity` after the first add).

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Segment | A new leaf segment `deck-wizard` (`WIZ`) | Fold into `ai-assist` (`AI`); fold into `deck-building` (`DECK`) | The wizard spans card data, deck rules, AI and synergy, owns its own flow, slicing and deferred list, and blocks nothing. In `ai-assist` it would bury deterministic deck policy under model plumbing; in `deck-building` it would pull a model call into a deterministic segment. Both neighbours keep their own small additions (`AI-036`, `AI-050`..`053`). |
| Who writes the deck | The player's Accept, through the existing add-card endpoint | A wizard endpoint that applies a whole step | The captain's rule is that the wizard never auto-generates a deck. Routing every card through the existing write path means one place that mutates decks and no way for a model response to change one. |
| Where the model sits | Curates a deterministic pool, one forced-tool call per step | An open tool loop with `search_cards`; the model proposes freely and the gate filters | A closed pool makes bracket, restriction and legality rules filters that hold by construction and keeps each step to one cheap call. An open loop would need the same filters on every tool result and would cost several calls per step. |
| Enforcing the bracket | Admission filters on the pool and the survivors, using `internal/bracket` | Prompt the model with the limits and check afterwards; reject over-limit adds in the decks API | Prompting is not enforcement. Rejecting adds contradicts the bracket check's report-only design (`DECK-125`) and would break manual building. Filtering what the wizard offers gives the guarantee where it is wanted. |
| Cumulative admission | Survivors admitted in order against a growing working deck | Check each proposal alone | Two cards can be fine alone and together complete a combo or exceed the Game Changer budget; the player may accept both. |
| Dual-land avoidance | A step the player can skip, plus a price cap | A "no duals" flag threaded through every prompt; a hard-coded cheap/expensive land list | Skipping is exact and free, and a pool with no lands cannot leak one. A price cap uses data the mirror already holds; a hand-kept list of expensive duals would go stale. |
| Land categories | Structural: produces two or more of the deck's colours; fetch lands by Oracle text or tag; utility lands by tag | Tag everything (`dual-land`, `shockland`, …) | Community tags are incomplete (the mirrored `dual-land` tag has no direct taggings), while `produced_mana` and Oracle text are Scryfall's own fields. A tag is used where there is no structural signal (utility lands) and as a second route for fetch lands, so a renamed tag cannot silently empty the category. |
| Price | Lowest USD among non-digital printings | Newest printing's price | The player's concern is what they would pay; the cheapest printing is the honest answer for a card with a pricey promo reprint. |
| Wizard state | One `deck_wizard_state` row per deck holding the answers as validated JSON; the deck stays the home of cards, bracket and archetype | Client-held answers only; a `wizard_sessions` table with per-session rows; typed columns for each answer; browser storage | A reload must resume the wizard, which browser storage would not do across devices or for the owner-gated deck. One row per deck needs no session lifecycle and goes with the deck. JSON mirrors the propose body, so one validator serves both and a new question adds no migration; the server never queries inside it. |
| Spend | A new `wizard` feature sharing the monthly ceiling, limit 40 | Count against `suggest`; unmetered because it is deterministic-heavy | One propose is one Sonnet call, so it is metered like suggest; a separate counter stops a wizard run from exhausting the suggest allowance and vice versa. 40 covers about five full runs a day. |
| Synergy cold start | Open-data (`edhrec_rank`) picks while synergy is `none` / `gathering`, with the status shown | Block the step until `ready`; fail | The synergy worker takes minutes to hours; the wizard must work meanwhile, and the response says which source it used. |
| Lands in the picks pool | Never | Allow high-synergy lands | A player who declined duals must not see one. Land picks belong to the land steps the player opted into. |
| Basics split | Largest remainder over pip demand, sources not netted | Frank Karsten source targets; netting nonbasic sources | Deterministic and explainable now; refinement waits for the role-filling steps, when the spells are known. |
| Role steps | One `roles` step, one role per propose, pool chosen by the existing `deckroles` classifier, count and ranges from `decktemplates` | A step per role; a new tag-backed classifier; letting the model name any role card from the whole mirror | The player picks which gap to fill, and the wizard shows the same counts as the deck-vs-target panel. A shared classifier means the count that drops when a card is accepted is the count the model was shown. The tag-backed classifier stays the `deck-building` deferred item. |
| Role room | Ask for up to `max − count`, empty at the maximum | Ask only up to `min − count`; always ask for a fixed number | The template range is the intent: below the minimum is a gap, inside the range is still the player's call, and at the maximum more of the role is not what the template wants. Ending at the maximum also spares a model call. |
| Role prefilter | A coarse SQL regex per role, then the Go classifier | Classify the whole mirror in Go; classify in SQL | The classifier is Go regexes over Oracle text and is authoritative; the SQL clause only bounds the rows read. Reimplementing the classifier in SQL would fork it. |
| Placement | Builder right-column panel replacing suggestions while open | A separate `/decks/{id}/wizard` page; a modal | The live decklist, image panel and template / bracket panels are the wizard's feedback; a separate page or modal would hide them. |

## Open Questions & Future Decisions

### Deferred (follow-up work)

1. **Tag-backed role classifier.** The roles step is only as complete as the Oracle-text classifier; swapping in a Scryfall-tag-backed `deckroles.Classifier` (`deck-building`'s deferred item) widens it with no change here.
2. **Bracket 4 fast mana.** A dedicated pass for fast mana, and the Bracket 4 land
   relaxation keyed on actually running it (`deck-building`'s deferred
   fast-mana-aware relaxation). Bracket 4 today gets the Game Changers (which include much of
   the fast mana) and the full combo allowance.
3. **Bracket 5.** Out of scope: cEDH is a metagame, not a card list.
5. **Partner commanders** (needs `SYN-051`).
6. **More land preferences.** Tapped-land tolerance, a snow-basics toggle, a
   land-count slider with the Bracket 4 relaxation.
7. **A chat surface.** Free-form questions to the model between steps.
8. **Constrained generation beyond these restrictions** (`AI-041`): "no infinite
   combos" as an explicit toggle at any bracket, theme locks, house rules.
9. **Bracket-sliced synergy** (`SYN-050`).
10. **Net-source basics** and Karsten-style per-colour source targets.
11. **Concurrent accepts.** Rapid double clicks on one proposal rely on the add
    endpoint incrementing quantity (and the singleton flag reporting it); the
    panel disables a row while its add is in flight.

### Gaps

12. **Quota is check-then-act**, as for the other AI features (`AI-031`).
13. **Pool narrowness.** A tight price cap or a thin synergy sample can leave a
    short pool; the response then carries fewer proposals and `pool_size` says
    why.

## References

- Code: `backend/internal/deckwizard/` (policy, land target, basics split,
  admission), `backend/internal/api/wizard.go`, `backend/internal/ai/` (`Curate`),
  `backend/internal/db/queries/wizard.sql`,
  `backend/internal/db/migrations/000010_add_wizard_ai_feature.up.sql`,
  `backend/internal/db/migrations/000011_create_deck_wizard_state.up.sql`,
  `frontend/src/components/builder/wizard/`, `frontend/src/lib/wizard.ts`
- Cross-segment: `deck-building` (`internal/bracket`, `internal/decktemplates`,
  `internal/deckstats`, `internal/deckrules`), `card-data` (oracle tags, price
  data, Game Changer flag, combo mirror), `ai-assist` (the gate, quotas, `Curate`),
  `synergy` (`Ensure`, `Commander`, attribution)
- Decision: `MANAFOLD.md` D8 (the wizard's slice and placement)
