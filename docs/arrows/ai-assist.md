# Arrow: ai-assist

The AI layer — Anthropic Claude wrapper, the `search_cards` tool over the mirror,
suggest-&-explain, single-card fit blurbs, deck-health prose, bracket estimate,
and the validate-every-returned-card gate.

## Status

**MAPPED** — LLD + specs authored 2026-09-02; M4 landed 2026-09-03.
`internal/ai` ships a real Anthropic-backed `Assistant` (`claude-sonnet-5` for
suggest, `claude-haiku-4-5` for explain) plus a `Disabled()` stub used when
`AI_ENABLED` is unset. `POST /api/decks/{id}/suggestions` and
`POST /api/decks/{id}/cards/{cardId}/explain` are live behind the
anti-hallucination gate, per-user daily quotas, and a global monthly spend
ceiling. Deck-health prose (AI-022, M5) and the bracket estimate (AI-023, M6)
remain gaps.

## References

### HLD
- docs/high-level-design.md (Approach — The model curates and explains; Tenets)

### LLD
- docs/intent/ai-assist/ai-assist-design.md

### EARS
- docs/intent/ai-assist/ai-assist-specs.md (AI-001..004, AI-010..013, AI-020..021, AI-024, AI-030..035; deferred AI-022..023, AI-040..043)

### Tests
- backend/internal/ai/ai_test.go — AI-001 (stub returns not-configured), AI-004 (per-feature model ids), cost estimate
- backend/internal/api/ai_test.go — AI-010, AI-011, AI-013, AI-020, AI-021, AI-024, AI-030 (billed-but-unusable + gate-error still metered), AI-031, AI-032, AI-033, AI-034, AI-035
- backend/internal/config/config_test.go — AI-002 (fail-fast on AI_ENABLED without a key)

### Code
- backend/internal/ai/ai.go (interface, types, `Disabled()` stub), anthropic.go (real assistant + tool loop), cost.go (price table)
- backend/internal/api/ai.go (`RegisterAIRoutes`, the two handlers, the gate, quota checks)
- backend/internal/db/queries/ai_usage.sql, backend/internal/db/queries/cards.sql (`ListSuggestionCandidates`)
- backend/internal/db/migrations/000005_create_ai_usage.up.sql
- backend/internal/config/config.go (`AIEnabled`, `AISuggestDailyLimit`, `AIExplainDailyLimit`, `AIMonthlySpendUSD`)
- backend/internal/server/server.go (`RegisterAIRoutes` in the authenticated `/api` group; `Deps.AI` is `ai.Assistant`)
- frontend/src/lib/api.ts (`suggestDeck`, `explainCard`), frontend/src/lib/deck.ts (`formatSuggestionsFooter`, `explainFitLabel`), frontend/src/app/(app)/decks/[id]/page.tsx (inline `SuggestionsPanel`; suggested names drive the shared card-image panel), frontend/src/components/builder/Decklist.tsx (inline `ExplainFit`, rendered through `DecklistRow`'s footer slot on the main / command boards)

## Architecture

**Purpose:** curate and explain on top of a deterministic engine; never invent a
card, never surface an illegal one.

**Key components:**
1. `Assistant` interface; a shared `anthropic.Client` built once from the
   developer key, or a `Disabled()` stub returning `ErrNotConfigured`.
2. Suggest runs a bounded `Messages.New` tool loop with a `search_cards` tool
   (results pre-filtered to the deck's colour identity and non-banned cards) and
   a `submit_suggestions` tool that ends the loop.
3. Explain is a single `claude-haiku-4-5` call, no tools.
4. The anti-hallucination gate — every returned name resolved against `cards`
   and re-validated with `internal/deckrules` for the deck's identity; failures
   dropped silently, never substituted, drop count reported.
5. `ai_usage` table backing per-user daily quotas and a global monthly spend
   ceiling; written whenever a model call returns token counts (metered before
   any error is mapped to a status), skipped only when the model was never
   reached.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Package & config | AI-001..004 | 4 | 0 | 0 |
| Anti-hallucination gate | AI-010..013 | 4 | 0 | 0 |
| Features | AI-020..021, AI-024 | 3 | 0 | 0 |
| Cost control | AI-030..035 | 6 | 0 | 0 |
| Deck-health / bracket | AI-022..023 | 0 | 0 | 2 (M5–M6) |
| EDHREC / constrained gen / cut suggestions / embeddings | AI-040..043 | 0 | 4 | 0 |

**Summary:** 17 of 23 implemented; 2 gaps (M5–M6); 4 deferred.

## Key Findings

1. Developer-held key, not BYOK — AI is a core feature (captain decision D1).
2. `AI_ENABLED` is an explicit switch, not inferred from the key's presence, so
   dev and CI build and boot with no key and an accidental key never turns paid
   features on silently.
3. Suggestions have no non-model fallback and return `502` on a provider error;
   the M5 deck-health report is the feature with a real deterministic fallback.
4. EDHREC high-synergy data (AI-040) is blocked on a Terms-of-Service decision:
   EDHREC has no official public API. Recorded as an open question in the LLD.

## Work Required

### Should Fix
1. Deck-health report (AI-022, M5) and bracket estimate (AI-023, M6).

### Nice to Have
1. Reserve-then-call quota semantics to close the check-then-act race.
2. `ai_usage` retention prune.
3. Resolve EDHREC ToS (AI-040); constrained generation (AI-041).
