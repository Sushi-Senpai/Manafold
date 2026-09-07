# AI Assist — EARS Specs

`internal/ai` ships a real Anthropic client from M4. A build with AI disabled
(`AI_ENABLED` unset) wires a stub `Assistant` whose call methods return
`ErrNotConfigured`, and every AI endpoint answers `503`. Deck-health prose
(AI-022) and the bracket estimate (AI-023) stay gaps for their milestones.

## Package & Configuration

- [x] **AI-001**: The system shall expose an `internal/ai` package defining an `Assistant` interface with `Suggest` and `Explain` methods, wired into `server.Deps`; when AI is disabled the wired value is a stub whose methods return `ErrNotConfigured`.
- [x] **AI-002**: If `AI_ENABLED` is `true` and `ANTHROPIC_API_KEY` is unset or empty at startup, then the API server shall fail to start with an error naming `ANTHROPIC_API_KEY`.
- [x] **AI-003**: When `AI_ENABLED` is `true`, the system shall build one shared Anthropic-backed `Assistant` at startup from the developer-held key and pass it to handlers via `server.Deps`, rather than constructing a client per request.
- [x] **AI-004**: The system shall send the "suggest & explain" requests to `claude-sonnet-5` and the single-card blurb requests to `claude-haiku-4-5`, the cheap-tier models the design assigns each feature.

## Anti-Hallucination Gate

- [x] **AI-010**: Before returning any card the language model named, the system shall resolve that name in the local mirror and re-validate the resolved card with `internal/deckrules` against the target deck's colour identity, and shall omit any card that does not resolve, is banned, or falls outside the deck's colour identity.
- [x] **AI-011**: The system shall assemble the suggestion candidate pool from a `cards`-table query ordered by `edhrec_rank`, filtered to the deck's colour identity and to cards not banned and not already in the deck — never from the language model's own enumeration of cards.
- [x] **AI-012**: The `search_cards` tool the suggestion loop exposes to the model shall filter every result to the deck's colour identity and to non-banned cards, so the model cannot see an illegal candidate.
- [x] **AI-013**: When the anti-hallucination gate drops one or more model-named cards, the system shall return the surviving cards without substituting replacements, and shall report how many were dropped.

## Features

- [x] **AI-020**: When an authenticated owner requests suggestions for a deck that has a commander assigned, the system shall return up to 10 real, non-banned cards within the deck's colour identity that are not already in it, each with a one-sentence rationale.
- [x] **AI-021**: When an authenticated owner requests an explanation for one card on a deck's main or command board, the system shall return a short rationale grounded in the commander's and the card's Oracle text and the deck's colour identity.
- [x] **AI-024**: If a suggestion request names a deck with no commander assigned, then the system shall respond `422` without calling the language model.

## Cost Control

- [x] **AI-030**: When a language-model call returns token usage, the system shall add one call plus its input-token, output-token, and estimated-cost totals to an `ai_usage` row keyed by user, calendar day, and feature, and shall do so before mapping any downstream error or gate failure to a response.
- [x] **AI-031**: While a user has reached the configured daily call limit for a feature, the system shall respond `429` to further calls of that feature for that user until the calendar day rolls over.
- [x] **AI-032**: While the sum of `ai_usage.cost_micros` for the current calendar month is at or above the configured ceiling, the system shall respond `503` to every AI endpoint until the ceiling is raised or the month rolls over.
- [x] **AI-033**: When an anonymous-draft caller (no authenticated user) calls any AI endpoint, the system shall respond `403`; AI features unlock on sign-in.
- [x] **AI-034**: The system shall meter a language-model call in `ai_usage` when and only when that call returned token usage: a call that never reached the model (the assistant disabled, or a transport failure before any response) is not counted, while a call that reached the model is counted even if its result is unusable — no parseable suggestions, a truncated payload, empty prose — or a later step fails.
- [x] **AI-035**: When a caller other than the deck's owner calls an AI endpoint scoped to that deck, the system shall respond `404` before evaluating the per-user daily limit or the global monthly ceiling, so a non-owner cannot probe quota or spend state through the response code.

## Deferred

- [ ] **AI-022**: When a client requests a deck-health report (M5), the system shall compute the deterministic analysis (curve, colour pips vs land sources, functional-category counts vs Commander rules-of-thumb) and return it, with an optional language-model prose summary and prioritized fix list layered on top that, if it fails, does not prevent the deterministic report from returning.
- [ ] **AI-023**: When a client requests a bracket estimate (M6), the system shall combine the Game-Changers count, a two-card-combo match against Commander Spellbook, and extra-turn / mass-land-denial detection into a 1–5 estimate, with a language-model explanation layered on top.
- [D] **AI-040**: The system shall use EDHREC high-synergy data for a commander/theme as a recommendation input, pending resolution of EDHREC's data-access approach and Terms of Service.
- [D] **AI-041**: When a client requests a generated deck under explicit constraints (budget ceiling, no infinite combos, no tutors, a tribe or theme lock, a free-text house rule), the system shall generate and validate the deck against those constraints in addition to Commander legality.
- [D] **AI-042**: When producing a deck-health report, the system shall rank the deck's current cards by synergy/impact and surface the lowest as cut candidates, each with a suggested replacement.
- [D] **AI-043**: The system shall support an embeddings-backed "cards similar to X" query.
