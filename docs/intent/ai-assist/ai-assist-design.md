---
parent: high-level-design
prefix: AI
---

# AI Assist

# NOTE: no AI code ships in M1. This LLD and its specs are drafted now so the
# `internal/ai` package stub, the schema hooks, and the milestone specs exist;
# implementation begins at M4.

## Context and Design Philosophy

The AI layer is what makes Manafold more than a legality-checking list editor. Its
governing rule is the HLD tenet *the model curates and explains; statistics and
rules decide*: the language model is an explainer and curator on top of a
deterministic engine. Recommendation *quality* comes from decklist statistics;
*legality* is enforced by re-validating every card the model returns against the
real card table and `internal/deckrules` for the current deck's colour identity
before it is shown — anything that fails is dropped silently. The model never
supplies the card list from its own memory and can never surface an illegal card.

The deterministic engines (`internal/deckstats`, `internal/deckrules`, later a
combo matcher) run **first and independently** and return a complete result with
no model involved; the LLM call is an enrichment layer that can fail without
breaking the feature.

## Provider & Key Model

**Anthropic Claude** via the official Go SDK
`github.com/anthropics/anthropic-sdk-go`. **Developer-held key**
(`ANTHROPIC_API_KEY`, required at startup from M4), not bring-your-own-key,
because AI assistance is a core always-on feature. Cost is controlled by:

- server-side **per-user daily quotas** (N analyze calls / M suggestion calls;
  tighter or zero for anonymous drafts),
- a **global monthly spend ceiling** with a kill-switch that feature-flags the
  AI endpoints off when exceeded,
- **prompt caching** on the static system prompt + the Commander-rules primer +
  the current deck snapshot,
- **cheap-tier models** where quality holds (`claude-haiku-4-5` for per-card
  blurbs; `claude-sonnet-5` for the main assistant and deck-health;
  `claude-opus-5` only for whole-deck generation).

A `ai_usage` table (`user_id`, `day`, `feature`, `count`, `input_tokens`,
`output_tokens`, `cost_estimate`) backs the quotas and the global ceiling.

## `internal/ai` Package Shape

- **M1 stub**: `ai.NewClient() *Client` returning a client whose call methods
  return `ErrNotConfigured`. `server.New` accepts it in `Deps` but no route uses
  it. This exists so `main.go`'s wiring and `server.Deps` do not churn at M4.
- **M4+**: one shared `*anthropic.Client` built once in `main.go` from the
  developer key and passed via `server.Deps` (developer-keyed, so — unlike a
  BYOK design — there is a package-level client). Two call shapes:
  1. **Tool-calling loop** — system prompt + deck snapshot + a `search_cards`
     tool over the Postgres mirror, capped at a small `maxToolIterations`. The
     tool's results are pre-filtered to the deck's colour identity so the model
     physically cannot see an illegal candidate.
  2. **Structured output** — a strict JSON schema for the fixed-shape responses
     (suggestion list, deck-health report, bracket estimate).

## Features & Endpoints (M4+)

| Endpoint | Feature | Deterministic part | Model part |
|---|---|---|---|
| `POST /api/decks/{id}/suggestions` | Suggest & explain (M4) | candidate pool: staples in the deck's colour identity not already in it, ranked by `edhrec_rank` (later: co-occurrence + EDHREC synergy) | curate 5–10, one-sentence rationale each; every returned id re-validated before display |
| `POST /api/decks/{id}/cards/{cardId}/explain` | Single-card fit blurb (M4) | — | 2–3 sentences over commander text + card text + deck theme |
| `POST /api/decks/{id}/analyze` | Deck-health report (M5) | `internal/deckstats`: curve, pips vs sources, ramp/draw/removal/wipe/land counts vs heuristics | prose summary + prioritized fixes |
| `POST /api/decks/{id}/bracket` | Bracket estimate (M6) | Game-Changers count, two-card-combo match (Commander Spellbook), extra-turn / mass-land-denial detection | explain the "vibe" and edge cases |

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Provider | Anthropic Claude | OpenAI (the sibling project's actual code) | The captain's explicit direction; Manafold has no OpenAI code to stay consistent with. |
| Key model | Developer-held key + server-side per-user quotas + a global spend ceiling | Bring-your-own-key | AI is a core feature, not a power-user add-on; a key-entry wall would gut adoption. A BYOK escape hatch may come later. |
| Anti-hallucination | Re-validate every model-returned card id against the card table + `internal/deckrules` for the deck's identity; drop failures silently | Prompt the model to stay grounded and trust it; a post-hoc "are these real?" model call | Prompting is not enforcement; the deterministic gate is the only thing that makes "never suggests an illegal card" true. Reusing `internal/deckrules` keeps one legality definition. |
| Candidate pool source (v1) | `edhrec_rank` within colour identity | LLM picks the pool from memory | The model's parametric card knowledge is stale and unreliable; the pool must come from data. Co-occurrence and EDHREC synergy extend it later. |
| Engine ordering | Deterministic engines run first and return a full result; the LLM call is optional enrichment | The LLM call is on the critical path for every AI feature | A deck-health report or suggestion list must still work when the model call fails or the spend ceiling is hit. |

## Open Questions & Future Decisions

### Deferred
1. **Deck-bootstrapping wizard** (captain bonus #1) — the "add the most
   powerful/common cards for these colours" step of `deck-building`'s wizard.
   Given a commander and a target count per functional slot, return ranked
   candidates within colour identity. Shares the candidate-pool machinery with
   suggestions; the difference is it fills a whole shell rather than topping up.
   Roadmap M8.
2. **EDHREC high-synergy data** (captain bonus #2) — use EDHREC's "high synergy
   cards" for a commander/theme as a recommendation input, not just
   `edhrec_rank`. **Open: EDHREC has no official public API.** The data-access
   approach (scrape with attribution and caching vs. a periodic bulk pull vs.
   asking EDHREC directly) and its **Terms of Service** must be settled before
   this is built — this is a genuine ask-user/legal question, not an engineering
   choice. Until then the recommendation engine runs on `edhrec_rank` + an own
   co-occurrence corpus only.
3. **Constrained deck generation beyond brackets** (captain bonus #3) —
   generate/validate against explicit constraints: a budget ceiling (`prices.usd`
   sum), "no infinite combos" (Commander Spellbook match), "no tutors" (a
   tutor tag), a tribe or theme lock (creature type / keyword), arbitrary house
   rules (a free-text constraint the model interprets and the deterministic
   layer spot-checks where it can). Needs a `GenerationConstraints` struct
   threaded through the tool-calling loop and the validation gate. Roadmap M7/M8.
4. **Cut suggestions / replacement discussion** (captain bonus #5) — the
   deck-health report's "prioritized fixes" surface underperforming or
   lowest-synergy cards as cut candidates, each with a suggested replacement;
   `deck-building` provides the low-friction swap. Needs a "rank current deck
   cards by synergy/impact" step (co-occurrence + EDHREC synergy once #2 is
   settled) feeding the M5 report. Roadmap M5+.
5. **Embeddings ("cards similar to X")** — Anthropic has no first-party
   embeddings API; this would mean Voyage AI or a local model. Deferred past v1.

### Gaps
6. **Quota enforcement for anonymous drafts** — anonymous callers have no
   `user_id` to hang a quota on; v1 answer is zero AI for anonymous drafts, AI
   unlocked on sign-in.
7. **A shared spend ceiling across instances** — the global cap needs a shared
   counter (Postgres row with a row lock) if the backend scales past one
   instance.

## References

- Code (M1): `backend/internal/ai/client.go` (stub only),
  `backend/internal/server/server.go` (`Deps.AI` field wired, no route).
- Code (M4+): `backend/internal/ai/`, `backend/internal/api/ai.go`,
  `backend/internal/db/queries/ai_usage.sql`.
- Cross-segment: reuses `deck-building`'s `internal/deckrules` as the validation
  gate; reads `card-data`'s `cards` table via the `search_cards` tool;
  `internal/deckstats` (`deck-building`) supplies the deterministic deck-health
  numbers. Commander Spellbook's open combo database is the M6 combo source.
- `claude-api` skill — model IDs, pricing, tool use, structured outputs, prompt
  caching.
