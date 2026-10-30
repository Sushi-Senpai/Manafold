# Deck Wizard — EARS Specs

Markers: `[x]` implemented, `[ ]` active gap, `[D]` deferred.

The wizard proposes; the player disposes. No requirement below lets the wizard
write a deck: every card enters through the builder's own add-card endpoint after
the player's click.

## Plan & Policy

- [x] **WIZ-001**: The system shall provide a bracket policy for Brackets 1 through 4 giving the bracket's Game Changer limit (0, 0, 3, unlimited — `internal/bracket`'s `GameChangerLimit`) and guidance text for the model, and shall report any other bracket, including 5, as unsupported.
- [x] **WIZ-002**: When the deck's owner requests `GET /api/decks/{id}/wizard`, the system shall return the wizard plan — `ready` with, when not ready, the first applicable `reason` of `no_commander`, `no_bracket`, or `bracket_unsupported`; the bracket, archetype, `ai_enabled`; the Game Changer limit, the count in the deck (the commanders plus every `main` and `command` entry, by quantity) and the number left; the land target and the base land target before any fast-mana relaxation, the lands on `main` and `command`, the basics needed, and whether fixing applies (the deck's colour identity has two or more colours); the commander's synergy status, and the fast-mana block of `WIZ-113` — and shall answer `404` to anyone else.
- [x] **WIZ-003**: The wizard's base land target shall be the floor of the midpoint of the chosen archetype's land range in `internal/decktemplates`, or 37 when the deck has no archetype; its land target shall be the base, except at Bracket 4, where `WIZ-114` lowers it.
- [x] **WIZ-004**: The wizard plan and the basic-land plan shall be answered without a language-model call, to an authenticated owner or an anonymous-draft owner alike, whether or not `AI_ENABLED` is set.
- [x] **WIZ-005**: When the plan is `ready` and a synergy service is configured, the system shall ask the synergy segment to ensure the commander so that gathering has started by the time the picks step runs, and shall report its status in the plan.

## Propose: Gates & Calls

- [x] **WIZ-010**: When a client calls `POST /api/decks/{id}/wizard/propose`, the system shall respond `503` if the assistant is disabled, then `403` to an anonymous-draft caller, then `404` to a caller who does not own the deck, then `503` if the global monthly ceiling is reached, then `429` if the caller has reached `AI_WIZARD_DAILY_LIMIT` calls of the `wizard` feature today, in that order and before any further work.
- [x] **WIZ-011**: If the deck has no commander, no bracket, or a bracket the policy does not support, then the system shall respond `422` naming the reason, without calling the language model.
- [x] **WIZ-012**: If the propose body names an unknown step, a `rejected_card_ids` entry that is not a UUID, more than 200 rejected ids, `notes` longer than 500 characters, a `max_price_usd` that is not a positive finite number, or a `land_target` outside 20 to 60, then the system shall respond `400` without calling the language model.
- [x] **WIZ-013**: If a step's pool is empty after every filter, or the step has nothing to ask for, then the system shall respond `200` with no proposals and an `empty_reason` of `land_target_met` (a land step when the deck already holds the land target), `no_fixing_needed` (the fixing step for a deck whose colour identity has fewer than two colours), `deck_full` (the picks step when `main` and `command` already hold 100 or more cards), or `no_candidates`, without calling the language model or metering a call.
- [x] **WIZ-014**: When a step has a non-empty pool, the system shall make exactly one `Curate` call to the language model, shall meter it under the `wizard` feature whenever it returned token usage, before mapping any error (`AI-030`, `AI-034`), and shall respond `502` if the call fails or returns no `submit_picks`, and `503` if the assistant reports it is not configured.
- [x] **WIZ-015**: A successful propose response shall carry the step, the model's summary, the source of the pool (`synergy`, `popular`, or `lands`), at most 10 proposals for `fixing` and `picks` and at most 5 for `utility`, each with its mirror card summary, the model's one-sentence reason, whether it is a Game Changer, its price, and its synergy numbers when it came from stored synergy data, plus the dropped count, the pool size, the Game Changer budget, whether combos were checked, and the model id.

## Gate & Admission

- [x] **WIZ-020**: Before returning any card the language model named, the system shall pass it through the same validate-every-returned-card gate as `suggestions` (`AI-010`), shall additionally drop any card that is not in the step's candidate pool, and shall report the number dropped without substituting any card.
- [x] **WIZ-021**: The system shall treat a candidate card as admissible for a target bracket only if, when it is a Game Changer, the deck's Game Changer count plus the candidate does not exceed the bracket's limit, and, whatever it is, adding it to the deck does not produce a `warning` finding from `internal/bracket` that the deck did not already have (a combo whose minimum bracket exceeds the target, mass land denial below Bracket 4, an extra-turn card in Bracket 1, two or more extra-turn cards below Bracket 4).
- [x] **WIZ-022**: The system shall build every pool from admissible candidates only, each tested alone against the deck, so that the language model is never shown a card the bracket does not admit.
- [x] **WIZ-023**: The system shall re-apply admission to the gated survivors in the model's order, each admitted card joining the working deck for the next test, and shall drop and count any that fail, so that accepting any subset of one response keeps the deck inside the bracket's Game Changer limit and combo expectations.
- [x] **WIZ-024**: While the Commander Spellbook combo mirror is empty, the system shall skip the combo part of admission, shall still enforce the Game Changer limit, and shall report `combos_checked: false`.
- [x] **WIZ-025**: The system shall not reject any deck mutation because of the wizard's bracket admission: admission limits what the wizard proposes, and `DECK-125` stays report-only.

## Restrictions & Rejections

- [x] **WIZ-030**: Where `restrictions.no_tutors` is true, the system shall remove from every pool each card carrying the `tutor` oracle tag or a descendant of it.
- [x] **WIZ-031**: Where `restrictions.max_price_usd` is set, the system shall remove from every pool each card whose lowest `prices.usd` among its non-digital printings exceeds the cap, and each card with no known USD price.
- [x] **WIZ-032**: The system shall pass `restrictions.notes` to the language model only as labelled, untrusted player preference text, and shall never let it change a pool, a filter, or the gate.
- [x] **WIZ-033**: The system shall never include a land (a card whose front face has the Land type) in the picks pool, whether from synergy data, `edhrec_rank`, or the Game Changer list, so that lands reach the player only through the land steps they chose.
- [x] **WIZ-034**: The system shall exclude from every pool each card already on any board of the deck, the commanders, and each card in `rejected_card_ids`.

## Pools

- [x] **WIZ-040**: The fixing pool shall be nonbasic lands within the deck's colour identity and commander-legal, ordered by ascending `edhrec_rank` with unranked cards last, that list at least two of the deck's colours in `produced_mana`, plus, where `include_fetch` is true, fetch lands — a land whose Oracle text sacrifices itself to search its controller's library, or one carrying the `fetchland` oracle tag or a descendant — up to 30 cards.
- [x] **WIZ-041**: The utility pool shall be lands within the deck's colour identity and commander-legal that carry the `utility-land` oracle tag or a descendant, ordered by ascending `edhrec_rank` with unranked cards last, up to 25 cards.
- [x] **WIZ-042**: A land step shall ask the model for no more cards than the land target leaves room for after the lands already on `main` and `command`.
- [x] **WIZ-043**: When the commander has a stored synergy sample with cards, the picks pool shall be built from `synergy.Service.Commander` (its legality-filtered mirror cards, in its order, restricted to the deck's colour identity) and the source shall be `synergy`; otherwise it shall be nonland commander-legal cards within the colour identity ordered by ascending `edhrec_rank`, and the source shall be `popular`.
- [x] **WIZ-044**: Where the bracket still admits Game Changers, the picks pool shall also include up to 10 commander-legal Game Changers within the deck's colour identity, by ascending `edhrec_rank`, flagged as Game Changers; where the Game Changer budget is exhausted it shall include none.
- [x] **WIZ-045**: When any proposal's data came from stored synergy data, the response shall carry the synergy status, the sample's deck count, and the attribution text "Synergy data from public Archidekt decks" with the link `https://archidekt.com` (`SYN-040`).

## Basic Lands

- [x] **WIZ-050**: When the deck's owner calls `POST /api/decks/{id}/wizard/basics`, the system shall return, without writing the deck, the basics needed (the land target, or the request's `land_target` of 20 to 60, minus the lands on `main` and `command`, not below 0) split across the deck's colour identity by largest-remainder allocation over weights equal to the greater of the colour's pip demand across `main` and `command` (the commanders included) and 1, breaking ties in W-U-B-R-G order, with each row's card summary, quantity to add, and quantity already in the deck.
- [x] **WIZ-051**: Where the deck's colour identity is empty, the basics plan shall use Wastes; where nothing is needed it shall return no rows.
- [x] **WIZ-052**: If a basic land the plan needs is not in the mirror, then the system shall omit that row and list the card's name under `missing`.
- [x] **WIZ-053**: The basics endpoint shall write nothing to the deck, and shall answer `400` to a `land_target` outside 20 to 60.
- [x] **WIZ-054**: If the deck has no commander, then the basics endpoint shall respond `422`, because the deck's colour identity is not known.

## Builder Surface

- [x] **WIZ-060**: The builder shall offer the wizard as a panel in the right column that replaces the AI suggestions panel while it is open, opened from a **Build with wizard** button in the deck header or by the URL parameter `wizard=1`, with the decklist, card-image panel, and left-column sections staying live; the decks list shall offer **New deck with wizard**, which creates a deck and opens it with `wizard=1`.
- [x] **WIZ-061**: The wizard's setup step shall show the deck's commander (and ask for one when missing), offer Brackets 1 through 4 only, an archetype, and the restrictions (no tutors, a price cap, strategy notes), and shall save the bracket and archetype to the deck with the existing deck update.
- [x] **WIZ-062**: The wizard's land steps shall ask whether to use basics only or also dual / fixing lands (and fetch lands), and whether to include utility lands, and shall make no propose request for a step the player declined or that does not apply.
- [x] **WIZ-063**: The wizard shall add a proposed card to the deck's `main` board only when the player clicks Accept for it, and a click on Reject shall exclude the card from every later round of that wizard session; no step shall add any card on its own.
- [x] **WIZ-064**: The wizard shall show the Game Changer budget (used of limit, or unlimited) and mark Game Changer proposals, and shall disable Accept on a Game Changer proposal while the budget left is zero.
- [x] **WIZ-065**: Where a picks response carries synergy data, the panel shall show the line "Synergy data from public Archidekt decks" linked to `https://archidekt.com` with the sample's deck count (`SYN-043`); where the source is `popular`, it shall say that popular-card picks are shown while synergy data is unavailable or still being gathered.
- [x] **WIZ-066**: The wizard's basics step shall show the plan and, on the player's click, add each row to `main` and set its quantity.
- [x] **WIZ-067**: When a wizard request fails, the panel shall say why in the player's terms — AI not configured, sign-in required, daily or monthly limit reached, provider failure — and shall leave setup and the basics step usable.

## Persisted Answers

The wizard's answers are saved per deck so a reload resumes where the player left off (`WIZ-092`). Saved answers are only what the wizard already asks; the deck itself still holds the commander, bracket, archetype and every accepted card.

- [x] **WIZ-092**: The system shall persist the wizard's setup restrictions, land and utility answers, land target, rejected cards and current step per deck, and the builder's wizard panel shall restore them when it opens, so that a reload resumes the wizard.
- [x] **WIZ-100**: When the deck's owner requests `GET /api/decks/{id}/wizard/state`, the system shall return `saved: false` with no state when nothing has been saved, or `saved: true` with the saved state (`step`, `restrictions`, `fixing`, `include_fetch`, `utility`, `land_target`, `rejected_card_ids`) and its `updated_at`, and shall answer `404` to anyone else, to an authenticated owner or an anonymous-draft owner alike, without a language-model call.
- [x] **WIZ-101**: When the deck's owner calls `PUT /api/decks/{id}/wizard/state`, the system shall replace the deck's saved state with the request body and return it, and shall answer `404` to anyone else.
- [x] **WIZ-102**: If the saved-state body names a `step` other than `setup`, `fixing`, `utility`, `picks`, or `basics`, a `fixing` other than `include` or `basics`, a `restrictions` value or `rejected_card_ids` that `WIZ-012` would refuse (a non-UUID entry, more than 200 entries, `notes` over 500 characters, a `max_price_usd` that is not a positive finite number), or a `land_target` outside 20 to 60, then the system shall respond `400` and leave the saved state unchanged.
- [x] **WIZ-103**: When the deck's owner calls `DELETE /api/decks/{id}/wizard/state`, the system shall remove the saved state and respond `204`, and shall answer `404` to anyone else; the deck's cards, bracket and archetype shall be left as they are.
- [x] **WIZ-104**: When a deck is deleted, the system shall delete its saved wizard state with it.
- [x] **WIZ-105**: When the wizard panel opens, it shall read the saved state before showing any step, restore the answers, rejected cards and step from it, and save the state again, after a short pause, whenever one of them changes, without ever saving before the read has finished; if the saved step cannot be shown for the deck as it now is, the panel shall open at setup.
- [x] **WIZ-106**: The wizard panel shall offer a **Start over** action that deletes the saved state and returns the panel to setup with default answers and no rejected cards, without changing the deck.
- [x] **WIZ-107**: If reading or saving the wizard state fails, then the panel shall say so and keep every step usable with the answers held in the page, and shall not save after a failed read, so a stored state it could not read is never overwritten by defaults.

## Bracket 4 Fast Mana

- [x] **WIZ-108**: The system shall count a card as fast mana only if it is a nonland card (front face not a Land) that carries the `mana-rock` or `ritual` oracle tag or a descendant of either, and has a mana value of 2 or less or is a Game Changer, using one definition for the pool and for every count of the deck's fast mana.
- [x] **WIZ-109**: If a propose request names the step `fastmana` while the deck's bracket is not 4, then the system shall respond `422` without building a pool or calling the language model; the plan shall report the fast-mana step as not applicable at those brackets, and no step, pool, guidance or land target at Brackets 1 through 3 shall differ from what it was without the fast-mana pass.
- [x] **WIZ-110**: The fast-mana pool shall be the fast-mana cards within the deck's colour identity and commander-legal, ordered by ascending `edhrec_rank` with unranked cards last, up to 20 cards, narrowed like every pool by `WIZ-022`, `WIZ-030`, `WIZ-031` and `WIZ-034`, so that the bracket's Game Changer budget and the player's price cap, tutor ban and rejections hold.
- [x] **WIZ-111**: When the fast-mana pool is non-empty, the system shall make exactly one `Curate` call for at most 8 picks, with guidance to favour the cheapest and most efficient fast mana the commander's plan can use without stacking one kind, shall vet the answer by `WIZ-020` and `WIZ-023`, and shall report the pool source as `fast_mana`.
- [x] **WIZ-112**: When the deck holds 100 or more cards on `main` and `command`, or the fast-mana pool is empty, the system shall respond `200` with no proposals and an `empty_reason` of `deck_full` or `no_candidates`, without calling the language model or metering a call.
- [x] **WIZ-113**: The wizard plan shall carry a `fast_mana` block: whether the step applies (the deck's bracket is 4) and the quantity of fast-mana cards on `main` and `command`, counted only at Bracket 4 and zero elsewhere.
- [x] **WIZ-114**: Where the bracket is 4, the land target shall be the base land target minus the floor of half the deck's fast-mana count, not below `decktemplates.LandRelaxedFloor` (32) and not above the base; at every other bracket it shall be the base. A `land_target` supplied in a request shall replace this target outright. The plan, the land steps' room (`WIZ-042`), and the basic-land plan shall all use it.
- [x] **WIZ-115**: The wizard shall not add any fast-mana card to the deck on its own: each enters the `main` board only through the player's Accept and the existing add-card endpoint, as `WIZ-063` requires.
- [x] **WIZ-116**: At Bracket 4 the panel shall include a *Fast mana* step between the utility lands and the picks, with Accept and Reject and the shared rejected set, shall show how the fast mana in the deck moved the land target (the count, the target and the base), and shall refresh that line as cards are accepted; at Brackets 1 through 3 and with no bracket the step shall be absent from the flow.

## Deferred

- [D] **WIZ-090**: The wizard shall fill the archetype template's role slots (ramp, card draw, targeted removal, board wipes) in dedicated steps.
- [x] **WIZ-091**: Where the bracket is 4, the wizard shall run a fast-mana pass and relax the land target by the amount of fast mana the deck actually runs.
- [D] **WIZ-093**: The wizard shall ask about tapped-land tolerance and snow basics.
