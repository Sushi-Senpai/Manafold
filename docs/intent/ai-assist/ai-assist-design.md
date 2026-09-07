---
parent: high-level-design
prefix: AI
---

# AI Assist

The AI layer is what makes Manafold more than a legality-checking list editor.
"Suggest & explain" and the single-card fit blurb ship at M4; the deck-health
report (M5) and bracket estimate (M6) reuse the same machinery and are drafted
here as future work.

## Context and Design Philosophy

The governing rule is the HLD tenet *the model curates and explains; statistics
and rules decide*: the language model is an explainer and curator on top of a
deterministic engine. Recommendation *quality* comes from decklist statistics;
*legality* is enforced by re-validating every card the model returns against the
real `cards` table and `internal/deckrules` for the current deck's colour
identity before it is shown — anything that fails is dropped silently. The model
never supplies the card list from its own memory and can never surface an
illegal card.

The deterministic parts run **first and independently**. For "suggest & explain"
the deterministic part is the candidate pool — a `cards` query ordered by
`edhrec_rank`, filtered to the deck's colour identity, non-banned, and not
already in the deck. The language-model call curates and explains that pool; if
it fails the endpoint returns an error rather than a fabricated list, because
suggestions have no meaningful non-model fallback. The M5 deck-health report
*does* have one — the deterministic analysis — and there the model call is pure
enrichment that can fail without breaking the response.

## Provider & Key Model

**Anthropic Claude** via the official Go SDK
`github.com/anthropics/anthropic-sdk-go`. **Developer-held key**
(`ANTHROPIC_API_KEY`), not bring-your-own-key, because AI assistance is a core
always-on feature.

A build sets `AI_ENABLED=true` to turn the features on. When it is true and
`ANTHROPIC_API_KEY` is missing, the server fails fast at startup naming the
variable (AI-002). When `AI_ENABLED` is unset — the default, and the state in
dev and CI — the wired `Assistant` is a stub whose methods return
`ErrNotConfigured` and every AI endpoint answers `503`, so the rest of the app
still builds and boots with no key present.

Cost is controlled by four mechanisms:

- **Per-user daily call quotas** per feature (`AI_SUGGEST_DAILY_LIMIT`,
  default 20; `AI_EXPLAIN_DAILY_LIMIT`, default 40). A user at the limit gets
  `429` until the calendar day rolls over (AI-031).
- **A global monthly spend ceiling** (`AI_MONTHLY_SPEND_USD`, default 50; 0
  disables the check). While the month-to-date sum of estimated cost is at or
  above it, every AI endpoint returns `503` (AI-032).
- **Cheap-tier models per feature** — `claude-sonnet-5` for "suggest & explain",
  `claude-haiku-4-5` for the single-card blurb (AI-004). Whole-deck generation,
  if it is ever built, is the only feature that would justify an Opus-tier
  model.
- **Anonymous drafts get no AI** — an anonymous-draft caller has no `user_id` to
  meter, so every AI endpoint returns `403` for them; the features unlock on
  sign-in (AI-033).

The `ai_usage` table (`user_id`, `usage_date`, `feature`, `calls`,
`input_tokens`, `output_tokens`, `cost_micros`, primary key on the first three)
backs both quotas. It is written **whenever a model call comes back with token
counts** — a call that reached the model has cost money and is metered even when
its result is unusable (no parseable suggestions, a truncated payload, empty
prose) or a later step fails; only a call that never reached the model (the
assistant disabled, a transport error before any response) escapes the meter
(AI-034). The assistant therefore returns the accumulated `Usage` on every
error path, and the handler meters it before mapping any error to a status
code. Cost is estimated from the returned token counts times a per-model price
table in `internal/ai`, stored in millionths of a USD for integer precision.

## `internal/ai` Package Shape

```go
type Assistant interface {
    Enabled() bool
    Suggest(ctx context.Context, req SuggestRequest) (SuggestResult, error)
    Explain(ctx context.Context, req ExplainRequest) (ExplainResult, error)
}
```

- **`Disabled()`** returns a stub whose `Suggest`/`Explain` return
  `ErrNotConfigured` and whose `Enabled()` is false. This is what `server.New`
  gets when `AI_ENABLED` is unset.
- **`NewAnthropic(apiKey string)`** returns the real assistant: one shared
  `anthropic.Client` built once (developer-keyed, so — unlike a BYOK design —
  there is a package-level client), reused for every request.

The handler owns the database and the anti-hallucination gate; the assistant
owns only the model conversation. So the request structs carry plain data plus,
for `Suggest`, a `SearchFunc` closure the handler builds over the mirror — the
assistant never imports the database package, which keeps it unit-testable with
a fake and keeps the gate in one place.

### Suggest — bounded tool loop

`SuggestRequest` carries the commander's name and Oracle text, the deck's colour
identity, the names already in the deck, the `edhrec_rank`-ordered candidate
pool (each entry name / cost / type / text / rank), and a
`SearchFunc(ctx, query) ([]CardRef, error)` that runs a mirror search already
filtered to the deck's colour identity and non-banned cards (AI-012).

The assistant runs a manual `Messages.New` loop with two tools:

- **`search_cards`** — input `{query}` in Manafold's mini-Scryfall syntax
  (`internal/cardsearch`); the handler's `SearchFunc` backs it.
- **`submit_suggestions`** — input `{suggestions: [{name, reason}]}`; calling it
  ends the loop. Using a tool rather than parsing free-text JSON out of the
  final message keeps the stop condition and the payload shape unambiguous.

The loop is capped at a small iteration count; if it ends without a
`submit_suggestions` call the assistant returns an error. Input/output token
counts are summed across turns into `SuggestResult.Usage`, which is populated on
every return path — success, parse failure, no-`submit_suggestions` — so the
handler can meter a call that reached the model regardless of its outcome.

### Explain — single call

`ExplainRequest` carries the commander name and text, the card's name and text,
and the deck's colour identity. One `Messages.New` call to `claude-haiku-4-5`,
no tools, a system prompt asking for two or three sentences grounded in the
commander and card text. `ExplainResult` is the prose plus `Usage`.

## Endpoints (M4)

| Endpoint | Feature | Deterministic part | Model part |
|---|---|---|---|
| `POST /api/decks/{id}/suggestions` | Suggest & explain | candidate pool: `ListSuggestionCandidates` — `edhrec_rank`-ordered, colour-identity ⊆ deck, non-banned, not in deck | curate up to 10 with a one-sentence rationale each; every returned name re-validated by the gate before display |
| `POST /api/decks/{id}/cards/{cardId}/explain` | Single-card fit blurb | the card must be on the deck's main or command board (else `404`) | 2–3 sentences over commander text + card text + deck colour identity |

Both endpoints, in order: `503` if the assistant is disabled; `403` if the
caller is an anonymous draft (AI-033); `404` if the deck is not the caller's
(shared `deckForOwner`, DECK-009); `503` if the global monthly ceiling is hit
(AI-032); `429` if the caller is at the feature's daily limit (AI-031).
Ownership is checked before the spend and quota gates so a non-owner always gets
`404` and can never probe the global spend state or their own quota state for a
deck they cannot see (AI-035). Then `suggestions` additionally returns `422` if
the deck has no commander (AI-024).
After the gates pass, the handler calls the assistant and then meters the
returned `Usage` if it carries any tokens (AI-030), **before** it inspects the
error. A transport failure before any response carries a zero `Usage` and is
not metered, then maps to `502`. A call that reached the model but came back
unusable — no parseable suggestions, a truncated `submit_suggestions` payload,
empty explanation prose — is metered and then mapped to `502`. A completed call
whose result the anti-hallucination gate cannot validate (a `banlist_overrides`
database error) is metered and then mapped to `500`. So neither the daily quota
(AI-031) nor the monthly ceiling (AI-032) is ever under-counted for a call that
cost money. On success the response is returned — for `suggestions`, the
surviving cards as `cardSummary` objects plus their rationale and a `dropped`
count (AI-013); for `explain`, the prose and the model id.

## The Anti-Hallucination Gate

`validateSuggestions` takes the model's `[]Suggestion` and the loaded deck and,
for each entry: resolves the name with `ResolveCardByName` (exact or
double-faced-card face match, the same resolver import uses); drops it if it
does not resolve. It then builds `deckrules.CardFacts` (with any
`banlist_overrides` row) and runs `deckrules.Validate` for a one-card deck at
the target deck's colour identity; drops the card if it has a colour-identity or
banlist violation. It drops cards whose id is already in the deck or already
kept in this pass (dedupe). Survivors keep their rationale. Nothing is
substituted for a dropped card, and the count of drops is returned (AI-013).
Reusing `deckrules` here is deliberate — there is exactly one definition of
"legal for this deck" in the codebase.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Provider | Anthropic Claude via `anthropic-sdk-go` | OpenAI | The captain's explicit direction; Manafold has no OpenAI code to stay consistent with. |
| Key model | Developer-held key + per-user daily quotas + a global monthly ceiling | Bring-your-own-key | AI is a core feature, not a power-user add-on; a key-entry wall would gut adoption. A BYOK escape hatch may come later. |
| Enable switch | Explicit `AI_ENABLED`; stub `Assistant` + `503` when unset | Infer "enabled" from the key being present | Dev and CI have no key and must still build and boot; an explicit switch makes AI-002's fail-fast unambiguous and keeps an accidental key in the environment from silently turning paid features on. |
| Anti-hallucination | Re-validate every model-returned name against the mirror + `deckrules` for the deck's identity; drop failures silently, never substitute | Prompt the model to stay grounded and trust it; a post-hoc "are these real?" model call | Prompting is not enforcement; the deterministic gate is the only thing that makes "never suggests an illegal card" true. Reusing `deckrules` keeps one legality definition. |
| Candidate pool source | `edhrec_rank` within colour identity, non-banned, not in deck | The model enumerates candidates from memory | The model's parametric card knowledge is stale and unreliable; the pool must come from data. Co-occurrence and EDHREC synergy extend it later. |
| Final answer shape | A `submit_suggestions` tool call | Parse a JSON block out of the model's last text message | A tool call has a schema and an unambiguous stop condition; free-text JSON needs a tolerant extractor and still fails on stray prose. |
| Models per feature | `claude-sonnet-5` for suggest, `claude-haiku-4-5` for explain | One model for both; Opus-tier throughout | The blurb is a short, low-stakes generation where Haiku holds quality at a fraction of the cost; suggestions need Sonnet's judgement over the pool. Opus-tier is reserved for whole-deck generation if it is ever built. |
| Suggestions on model failure | Return `502` (no fallback list) | Fall back to the raw `edhrec_rank` pool | An unexplained top-`edhrec` dump is not what the user asked for and reads as a broken feature; better to surface the failure. The M5 deck-health report is the case *with* a real deterministic fallback. |
| Usage accounting | The assistant returns its accumulated `Usage` on every path; the handler writes `ai_usage` whenever that `Usage` carries tokens, before it maps any error to a status code | Reserve quota before the call; write only after the whole handler succeeds; meter only the clean success path | A call that never reached the model (assistant disabled, transport error before any response) carries a zero `Usage` and should not cost the user a call, but any call that reached the model has incurred real cost and must be metered regardless of an unusable result or a downstream gate failure — otherwise a deterministically repeatable path (model replies with plain text, `MaxTokens` truncation, a `banlist_overrides` blip) silently under-counts the ceiling. The small risk is a burst of concurrent calls slipping a few over the limit, which is acceptable for v1. |
| Quota store | A Postgres `ai_usage` table, checked per request | An in-process counter | Quotas must survive a restart and, when the backend scales past one instance, be shared; a table is both. The month-to-date ceiling sum is a single indexed aggregate. |

## Open Questions & Future Decisions

### Gaps

- **Check-then-act races on both quotas.** The daily limit and the monthly
  ceiling are read, then the call is made, then usage is written. Concurrent
  requests from one user can slip a few calls over the daily limit, and the
  global ceiling can be overshot by roughly one in-flight batch of calls. For a
  single small instance this is immaterial; a stricter design would reserve a
  slot in the same statement that reads the count, or move the ceiling to a
  row-locked counter.
- **`ai_usage` has no retention policy.** Rows accumulate one per user/day/
  feature forever. A periodic prune of rows older than the ceiling's lookback
  window (currently one month) is a later cron.
- **Deck theme is only the commander.** The suggestion prompt describes the deck
  by its commander and colour identity, not by a detected archetype or the
  functional-category mix. Feeding `internal/deckstats` output and an archetype
  guess into the prompt is an M5+ refinement.

### Deferred

1. **Deck-bootstrapping wizard** (captain bonus #1) — given a commander and a
   target count per functional slot, return ranked candidates within colour
   identity to fill a whole shell rather than top up an existing deck. Shares
   the candidate-pool machinery with suggestions. Roadmap M8.
2. **EDHREC high-synergy data** (captain bonus #2) — use EDHREC's "high synergy
   cards" for a commander/theme as a recommendation input beyond `edhrec_rank`.
   **Open: EDHREC has no official public API.** The data-access approach (scrape
   with attribution and caching vs. a periodic bulk pull vs. asking EDHREC
   directly) and its Terms of Service must be settled before this is built — a
   genuine ask-user/legal question, not an engineering choice. Until then the
   recommendation engine runs on `edhrec_rank` plus an own co-occurrence corpus
   only.
3. **Constrained deck generation beyond brackets** (captain bonus #3) —
   generate/validate against explicit constraints: a budget ceiling
   (`prices.usd` sum), "no infinite combos" (Commander Spellbook match), "no
   tutors" (a tutor tag), a tribe or theme lock, arbitrary free-text house rules
   the model interprets and the deterministic layer spot-checks where it can.
   Needs a `GenerationConstraints` struct threaded through the tool loop and the
   gate. Roadmap M7/M8.
4. **Cut suggestions / replacement discussion** (captain bonus #5) — the
   deck-health report's "prioritized fixes" surface underperforming or
   lowest-synergy cards as cut candidates, each with a suggested replacement;
   `deck-building` provides the low-friction swap. Needs a "rank current deck
   cards by synergy/impact" step feeding the M5 report. Roadmap M5+.
5. **Embeddings ("cards similar to X")** — Anthropic has no first-party
   embeddings API; this would mean Voyage AI or a local model. Deferred past v1.
6. **Prompt caching** on the static system prompt + the Commander-rules primer
   once the prompts settle, and a shared row-locked spend counter if the
   backend scales past one instance.

## References

- Code (M4): `backend/internal/ai/` (`ai.go`, `anthropic.go`, `cost.go`),
  `backend/internal/api/ai.go`, `backend/internal/db/queries/ai_usage.sql`,
  `backend/internal/db/migrations/000005_create_ai_usage.up.sql`.
- Cross-segment: reuses `deck-building`'s `internal/deckrules` as the validation
  gate and its `ResolveCardByName` / `loadDeck`; reads `card-data`'s `cards`
  table via `ListSuggestionCandidates` and the `search_cards` tool
  (`internal/cardsearch`). `platform-shell` supplies the config toggles and
  registers `RegisterAIRoutes` inside the authenticated `/api` group.
- `claude-api` skill — model IDs, pricing, tool use, prompt caching.
- Commander Spellbook's open combo database is the M6 combo source.
