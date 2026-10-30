# Arrow: deck-wizard

The interactive deck-building wizard: setup (commander, bracket 1–4,
restrictions) → land preferences, including "basics only" → utility lands →
rounds of synergy picks the player accepts or rejects → a deterministic basics
plan. The backend builds each step's candidate pool from the mirror and holds it
to the bracket and the player's rules; one forced-tool model call picks from the
pool and explains; the picks pass the existing validate-every-returned-card gate.
The wizard never writes a deck — every card enters through the player's Accept.

## Status

**MAPPED** — LLD + specs authored and the first slice landed 2026-10-29
(WIZ-001..067, AI-036, AI-050..053, SYN-043); persisted wizard answers
(WIZ-092) landed 2026-10-30, followed by the Bracket 4 fast-mana pass and
fast-mana-aware land target (WIZ-091, WIZ-108..116). Deferred: role-filling
steps (WIZ-090), tapped-land and snow-basics questions (WIZ-093). Bracket 5 is
out of scope. The panel's logic is unit-tested; its rendering was not exercised in a
browser in this environment, and the backend was exercised end to end over HTTP
against real card data with a stand-in model. The fast-mana queries and API
tests were written without a Postgres instance available and have not run.

## References

### HLD
- docs/high-level-design.md (Key Design Decisions — the deck wizard is interactive and filter-first; Roadmap — later features 1–3)
- MANAFOLD.md D8 (the wizard's slice, placement and spend)

### LLD
- docs/intent/deck-wizard/deck-wizard-design.md

### EARS
- docs/intent/deck-wizard/deck-wizard-specs.md (WIZ-001..005, WIZ-010..015, WIZ-020..025, WIZ-030..034, WIZ-040..045, WIZ-050..054, WIZ-060..067, WIZ-091, WIZ-092, WIZ-100..116; WIZ-090, 093 deferred)

### Tests
- backend/internal/deckwizard/deckwizard_test.go — WIZ-001 (policy for brackets 1–4, none for 5), WIZ-002 (Game Changers left), WIZ-003 (land target per archetype), WIZ-012 (land-target range), WIZ-021 (Game Changer limit, combos, mass land denial, extra turns, a deck already over the limit), WIZ-023 (cumulative budget, pairs that only conflict together), WIZ-050 / WIZ-051 (largest-remainder basics split, Wastes, zero rows), WIZ-109 (fast mana applies at Bracket 4 only), WIZ-114 (relaxed land target: one per two pieces, floor 32, never above base, base off Bracket 4)
- backend/internal/api/wizard_test.go (needs Postgres) — WIZ-002..005 (plan readiness, policy and land numbers, owner-only, no AI needed, synergy ensure), WIZ-010..015 (gate order, 422, 400, empty answers skip the model, metering on every path, step caps), WIZ-020 (gate + pool membership), WIZ-021..025 (Game Changer budget by bracket, combo admission, combos_checked, never blocking an add), WIZ-030..034 (tutor ban, price cap and cheapest printing, notes, no lands in picks, rejected and on-deck exclusion), WIZ-040..045 (fixing and utility pools, land room, synergy-vs-popular source, attribution), WIZ-050..054 (basics plan, no writes, Wastes, missing, 422), WIZ-108..106 (fast-mana definition and pool, 422 below Bracket 4 without a model call, gate and metering, plan block, relaxed target reaching land steps and basics, override wins)
- backend/internal/ai/curate_test.go, ai_test.go — AI-050..053 (forced `submit_picks`, no search tool, cap and blank-drop, untrusted notes in the prompt, usage on every path, zero usage on a transport error), AI-051
- backend/internal/config/config_test.go — AI-036 (`AI_WIZARD_DAILY_LIMIT` default and fail-safe)
- backend/internal/db/migrate_test.go — AI-036 (`ai_usage` admits `wizard`, still refuses an unknown feature)
- frontend/src/lib/wizard.test.ts — WIZ-031 / WIZ-012 (price cap parsing), WIZ-030..034 (request body), WIZ-061 (brackets 1–4), WIZ-062 (fixing skipped for under two colours), WIZ-063 (rejected set), WIZ-064 (Game Changer budget and Accept gate), WIZ-065 (attribution vs popular note), WIZ-013 / WIZ-067 (messages), WIZ-066 (basics apply plan), WIZ-108 (fast-mana step only at Bracket 4, the land-target line)

### Code
- backend/internal/deckwizard/deckwizard.go (`PolicyFor`, `GameChangersLeft`, `LandTarget`, `RelaxedLandTarget`, `SplitBasics`, `Admit`, `AdmitInOrder`, `Step`)
- backend/internal/api/wizard.go (`RegisterWizardRoutes`, `getWizardPlan`, `wizardPropose`, `buildWizardPool`, `vetPicks`, `wizardBasics`); backend/internal/api/ai.go (`gateNames`, shared with suggest); stats.go (`statsInputFor`) and bracket.go (`bracketDeckCards`) share their deck views
- backend/internal/ai/curate.go (`Curate`, `curatePrompt`), ai.go (`CurateRequest`, `PoolCard`, `Assistant.Curate`)
- backend/internal/db/queries/wizard.sql (`ListWizardFastMana`, `FilterFastManaCardIDs`); backend/internal/db/migrations/000010_add_wizard_ai_feature.up.sql / .down.sql
- backend/internal/config/config.go (`AIWizardDailyLimit`), cmd/api/main.go, internal/server/server.go, backend/.env.example, render.yaml
- frontend/src/lib/wizard.ts, frontend/src/lib/api.ts (`getWizardPlan`, `wizardPropose`, `wizardBasics`), frontend/src/components/builder/wizard/ (`WizardPanel`, `ProposalList`), frontend/src/app/(app)/decks/[id]/page.tsx (header button, `?wizard=1`, panel swap), frontend/src/app/(app)/decks/page.tsx (New deck with wizard)

## Architecture

**Purpose:** let the player build with the model without ever handing it the
rules. The deterministic side decides what may be offered (legality, price,
tutors, Game Changer budget, combo / mass-land-denial / extra-turn warnings from
`internal/bracket`); the model chooses among that menu and explains; the player
accepts or rejects.

**Key components:**
1. `internal/deckwizard` — bracket policy, land target, basics split, and
   admission over `internal/bracket` (each candidate alone for the pool,
   cumulatively for the survivors).
2. Pool queries (`wizard.sql`) and the picks pool from `synergy.Service.Commander`
   with an `edhrec_rank` fallback.
3. `ai.Assistant.Curate` — one forced `submit_picks` call; metered under the
   `wizard` feature.
4. The shared gate (`gateNames`) plus pool membership and cumulative admission
   in `vetPicks`.
5. The builder's right-column panel, which replaces the suggestions panel while
   open.

## Key Findings

1. `ai_usage.feature` carried a database check limited to the two original
   features, so the wizard needed a migration, not just a new feature string.
2. Oracle tags are community data and thin in places (the `utility-land` tag has
   few taggings; the `fetchland` slug could not be confirmed offline), so fetch
   lands are also recognised from Oracle text and utility lands are the one
   category that depends on a tag alone.
3. Skipping a step costs nothing: "basics only", no utility lands, a mono-colour
   deck, a met land target, a full deck and an empty pool all answer without a
   model call.

4. Fast mana is a tag rule (`mana-rock` / `ritual`, mana value 2 or less or a
   Game Changer) shared by the pool and the deck count through two SQL queries;
   the land relaxation counts the deck's own cards, so it survives reloads and
   removals without wizard state. The `ritual` slug could not be confirmed
   offline.

## Work Required

### Should Fix
1. Role-filling steps against the archetype template (WIZ-090).

### Nice to Have
1. Verify the `fetchland`, `utility-land`, `mana-rock` and `ritual` tag coverage
   against a full tag export and widen the utility category (man-lands) if it is
   thin.
2. Weight fast mana by strength, and count dorks and extra-mana lands, in the
   land relaxation.
