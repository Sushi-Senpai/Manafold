# AI Assist — EARS Specs

No AI code ships in M1. Every spec below is a gap for its milestone; the
`internal/ai` package exists in M1 as a stub whose call methods return
`ErrNotConfigured`.

## Package & Configuration

- [x] **AI-001**: The system shall expose an `internal/ai` package whose constructor is wired into `server.Deps` from M1, with its call methods returning a not-configured error until the Anthropic client is enabled at M4.
- [ ] **AI-002**: If `ANTHROPIC_API_KEY` is unset at startup once the AI features are enabled, then the system shall fail to start with a message naming the missing variable.
- [ ] **AI-003**: The system shall build one shared Anthropic client at startup from the developer-held key and pass it to handlers via `server.Deps`, rather than constructing a client per request.

## Anti-Hallucination Gate

- [ ] **AI-010**: Before returning any card the language model named, the system shall look that card up in the local mirror and re-validate it against `internal/deckrules` for the target deck's colour identity, and shall omit any card that is not found, is banned, or is outside the deck's colour identity.
- [ ] **AI-011**: The system shall source every recommendation candidate pool from decklist statistics (`edhrec_rank`, later a co-occurrence corpus and EDHREC synergy data), never from the language model's own enumeration of cards.

## Features

- [ ] **AI-020**: When a client requests suggestions for a deck, the system shall return 5–10 real, legal cards within the deck's colour identity that are not already in it, each with a one-sentence rationale.
- [ ] **AI-021**: When a client requests an explanation for one card in a deck, the system shall return a short rationale grounded in the commander's and the card's Oracle text and the deck's theme.
- [ ] **AI-022**: When a client requests a deck-health report, the system shall compute the deterministic analysis (curve, colour pips vs land sources, functional-category counts vs Commander rules-of-thumb) and return it, with an optional language-model prose summary and prioritized fix list layered on top that, if it fails, does not prevent the deterministic report from returning.
- [ ] **AI-023**: When a client requests a bracket estimate, the system shall combine the Game-Changers count, a two-card-combo match against Commander Spellbook, and extra-turn / mass-land-denial detection into a 1–5 estimate, with a language-model explanation layered on top.

## Cost Control

- [ ] **AI-030**: The system shall record every language-model call's feature, token counts, and cost estimate in an `ai_usage` table keyed by user and day.
- [ ] **AI-031**: While a user has reached their daily quota for a feature, the system shall respond `429` to further calls of that feature for that user until the day rolls over.
- [ ] **AI-032**: While the global monthly spend estimate exceeds the configured ceiling, the system shall respond `503` to every AI endpoint until the ceiling is raised or the month rolls over.
- [ ] **AI-033**: The system shall grant anonymous-draft callers no AI quota; AI features unlock on sign-in.

## Deferred

- [D] **AI-040**: The system shall use EDHREC high-synergy data for a commander/theme as a recommendation input, pending resolution of EDHREC's data-access approach and Terms of Service.
- [D] **AI-041**: When a client requests a generated deck under explicit constraints (budget ceiling, no infinite combos, no tutors, a tribe or theme lock, a free-text house rule), the system shall generate and validate the deck against those constraints in addition to Commander legality.
- [D] **AI-042**: When producing a deck-health report, the system shall rank the deck's current cards by synergy/impact and surface the lowest as cut candidates, each with a suggested replacement.
- [D] **AI-043**: The system shall support an embeddings-backed "cards similar to X" query.
