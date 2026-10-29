---
parent: high-level-design
prefix: WIZ
---

# Deck Wizard

The deck wizard is an interactive scaffolder. A player names a commander, a
Commander Bracket, and any restrictions, and the wizard walks them through the
build one question at a time: which kinds of lands they will play, which utility
lands they want, and then a stream of synergy picks they accept or reject, all
inside the bracket's Game Changer and combo limits. **It never generates a
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

The wizard ships as the smallest end-to-end path that works. Five steps, in
order; the player can stop after any of them and carry on in the builder.

| # | Step | Player is asked | Candidate pool (deterministic) | Model's job | Who writes the deck |
|---|---|---|---|---|---|
| 0 | **Setup** | commander (already on the deck), bracket 1–4, optional archetype, restrictions (no tutors, price cap per card, free-text strategy notes) | — | none | the existing `PATCH /api/decks/{id}` for bracket and archetype |
| 1 | **Fixing lands** | "Basics only, or include dual / fixing lands?" and "fetch lands too?" | nonbasic lands that make two or more of the deck's colours, plus fetch lands if allowed | pick up to 10 and explain | Accept → existing add-card |
| 2 | **Utility lands** | "Any utility lands (man-lands, colourless utility)?" | lands tagged `utility-land` (or a descendant) | pick up to 5 and explain | Accept → existing add-card |
| 3 | **Picks** | nothing: the player reviews rounds of up to 10 proposals and accepts or rejects each; "more" asks for another round | synergy-ranked cards (Archidekt), or `edhrec_rank`-ranked while synergy is gathering, plus the Game Changers the bracket still allows | pick up to 10 and explain, steered by the strategy notes | Accept → existing add-card |
| 4 | **Basics** | "Fill the rest with basics?" | the remaining land slots, split by colour-pip demand | none (deterministic) | the player's click → existing add-card |

**Dual-land avoidance** is a choice, not a filter on the model: a player who
picks "basics only" makes no step-1 call at all, and no later step ever proposes
a land — the picks pool is nonland cards only (`WIZ-033`) — so a player who does
not want duals never sees one. The price cap addresses the other common worry:
the player who is fine with duals but not with expensive ones.

**Interactivity** is structural. A proposal list changes the deck only through
the player's Accept click; a Reject adds the card to a session-only rejected set
that later rounds exclude. There is no "build it for me" path in the API or the
UI.

**Deferred past this slice** (listed under *Open Questions & Future Decisions*):
role-filling steps (ramp, draw, removal, wipes), Bracket 5, persistence of
wizard answers across reloads, partner commanders, a fast-mana-specific pass
for Bracket 4, and a free-form chat with the model.

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

Wizard answers (restrictions, land preferences, the rejected set) live in the
panel's React state for the page's lifetime. The deck itself, which holds the
commander, bracket, archetype and every accepted card, is the only durable
state. Reloading restarts the wizard at setup with the deck's bracket and
archetype prefilled.

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

A full run is about eight calls: at most one each for fixing and utility, then a
round per ten picks. Choosing "basics only", skipping utility lands, a mono-colour
deck, a met land target, a full deck, an empty pool, the plan and the basics
split all cost no model call.

## HTTP API

All under the protected `/api` group, registered by `RegisterWizardRoutes`.

| Endpoint | Method | Gates | Purpose |
|---|---|---|---|
| `/api/decks/{id}/wizard` | GET | ownership (`404`) | the plan: readiness, bracket policy and Game Changer budget, land target and counts, synergy status |
| `/api/decks/{id}/wizard/propose` | POST | AI gates (`503`/`403`/`404`/`503`/`429`) | one curated round for a step |
| `/api/decks/{id}/wizard/basics` | POST | ownership (`404`) | the basic-land plan; writes nothing |

```json
// GET → WizardPlan
{ "ready": true, "reason": null, "ai_enabled": true,
  "bracket": 3, "archetype": "midrange_value",
  "policy": { "game_changer_limit": 3, "game_changers_in_deck": 1,
              "game_changers_left": 2, "guidance": "…" },
  "lands": { "target": 37, "in_deck": 12, "basics_needed": 25,
             "fixing_applicable": true },
  "synergy": { "status": "gathering", "reason": null } }
// reason: "no_commander" | "no_bracket" | "bracket_unsupported" when not ready

// POST propose → body
{ "step": "picks",                      // "fixing" | "utility" | "picks"
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
  five steps (setup, fixing lands, utility lands, picks, basics) and the round
  result shared by the three curated steps; `ProposalList.tsx` — one round's
  cards with Accept and Reject.
- `frontend/src/lib/wizard.ts` — pure, tested: step order and skipping rules,
  the rejected-set reducer, request-body builders, the Game Changer budget line,
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
| Wizard state | Client-held answers, the deck as the only durable state | A `wizard_sessions` table | A session lasts minutes and the deck already holds everything that matters. A table would add a migration, ownership queries and cleanup for no recovered value in this slice. |
| Spend | A new `wizard` feature sharing the monthly ceiling, limit 40 | Count against `suggest`; unmetered because it is deterministic-heavy | One propose is one Sonnet call, so it is metered like suggest; a separate counter stops a wizard run from exhausting the suggest allowance and vice versa. 40 covers about five full runs a day. |
| Synergy cold start | Open-data (`edhrec_rank`) picks while synergy is `none` / `gathering`, with the status shown | Block the step until `ready`; fail | The synergy worker takes minutes to hours; the wizard must work meanwhile, and the response says which source it used. |
| Lands in the picks pool | Never | Allow high-synergy lands | A player who declined duals must not see one. Land picks belong to the land steps the player opted into. |
| Basics split | Largest remainder over pip demand, sources not netted | Frank Karsten source targets; netting nonbasic sources | Deterministic and explainable now; refinement waits for the role-filling steps, when the spells are known. |
| Placement | Builder right-column panel replacing suggestions while open | A separate `/decks/{id}/wizard` page; a modal | The live decklist, image panel and template / bracket panels are the wizard's feedback; a separate page or modal would hide them. |

## Open Questions & Future Decisions

### Deferred (follow-up work)

1. **Role-filling steps.** Ramp, card draw, targeted removal and board wipes
   against the archetype template, using the oracle-tag classifier
   (the tag-backed classifier in `deck-building`'s deferred list; the
   `deckroles.Classifier` interface already allows it). The slice leaves these to
   the picks step and the builder's "deck vs. target".
2. **Bracket 4 fast mana.** A dedicated pass for fast mana, and the Bracket 4 land
   relaxation keyed on actually running it (`deck-building`'s deferred
   fast-mana-aware relaxation). Bracket 4 today gets the Game Changers (which include much of
   the fast mana) and the full combo allowance.
3. **Bracket 5.** Out of scope: cEDH is a metagame, not a card list.
4. **Persistence.** Saving restrictions and land answers on the deck so a reload
   resumes the wizard.
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
  `frontend/src/components/builder/wizard/`, `frontend/src/lib/wizard.ts`
- Cross-segment: `deck-building` (`internal/bracket`, `internal/decktemplates`,
  `internal/deckstats`, `internal/deckrules`), `card-data` (oracle tags, price
  data, Game Changer flag, combo mirror), `ai-assist` (the gate, quotas, `Curate`),
  `synergy` (`Ensure`, `Commander`, attribution)
- Decision: `MANAFOLD.md` D8 (the wizard's slice and placement)
