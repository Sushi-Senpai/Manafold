# Arrow: ai-assist

The AI layer — Anthropic Claude wrapper, the `search_cards` tool over the mirror,
suggest-&-explain, deck-health prose, bracket estimate, and the
validate-every-returned-card gate. No AI code in M1 beyond an `internal/ai` stub.

## Status

**DRAFT** — LLD + specs authored 2026-09-02. Only the `internal/ai` stub (call
methods return `ErrNotConfigured`) is wired into `server.Deps`. Implementation
begins at M4.

## References

### HLD
- docs/high-level-design.md (Approach — The model curates and explains; Tenets)

### LLD
- docs/intent/ai-assist/ai-assist-design.md

### EARS
- docs/intent/ai-assist/ai-assist-specs.md (AI-001..003, AI-010..011, AI-020..023, AI-030..033, AI-040..043)

### Tests
- backend/internal/ai/client_test.go — AI-001 (stub returns not-configured)

### Code
- backend/internal/ai/client.go (stub)
- backend/internal/server/server.go (`Deps.AI` field)
- (M4+) backend/internal/ai/, backend/internal/api/ai.go, backend/internal/db/queries/ai_usage.sql

## Architecture

**Purpose:** curate and explain on top of a deterministic engine; never invent a
card, never surface an illegal one.

**Key components (M4+):**
1. Shared `*anthropic.Client` built once from the developer key.
2. Tool-calling loop with a `search_cards` tool whose results are pre-filtered to
   the deck's colour identity.
3. Structured-output calls for fixed-shape responses.
4. The anti-hallucination gate — every returned card re-validated against `cards`
   + `internal/deckrules` for the deck's identity; failures dropped silently.
5. `ai_usage` table backing per-user daily quotas and a global spend ceiling
   with a kill-switch.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Package & config | AI-001..003 | 1 | 0 | 2 (M4) |
| Anti-hallucination gate | AI-010..011 | 0 | 0 | 2 (M4) |
| Features | AI-020..023 | 0 | 0 | 4 (M4–M6) |
| Cost control | AI-030..033 | 0 | 0 | 4 (M4) |
| EDHREC / constrained gen / cut suggestions / embeddings | AI-040..043 | 0 | 4 | 0 |

**Summary:** 1 of 16 implemented; 11 gaps (M4–M6); 4 deferred.

## Key Findings

1. Developer-held key, not BYOK — AI is a core feature (captain decision D1).
2. EDHREC high-synergy data (AI-040) is blocked on a Terms-of-Service decision:
   EDHREC has no official public API. Recorded as an open question in the LLD.
3. The deterministic engines run first and return a full result; the LLM call is
   optional enrichment that can fail without breaking the feature.

## Work Required

### Must Fix
(none — M1 scope is the stub only)

### Should Fix
1. Implement AI-001..033 across M4–M6.

### Nice to Have
1. Resolve EDHREC ToS (AI-040); constrained generation (AI-041).
