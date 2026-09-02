## LID
- Mode: Full
- Version: 1.3.0

## Linked-Intent Development (MANDATORY)

**Consult the `linked-intent-dev` skill for ALL code changes.** All changes flow through the arrow of intent in one direction:

```
HLD → LLDs → EARS → Tests → Code
```

- **New features and refactors**: full six-phase workflow (HLD check → LLD check/draft → EARS → intent-narrowing edge audit → tests-first → code).
- **Bug fixes**: walk the arrow like any other change — find where behavior diverged from intent and cascade from there. No short-circuit.
- **If unsure**: use the full workflow.

Stop after each phase for user review. **Docs carry current intent, written to be read cold** — write each doc as if authored fresh today, from current intent alone: no narration of how it changed, no meaning that needs the conversation that produced it, no rebuttals to questions only a past discussion raised. Rationale, considered alternatives, and constraints a fresh author would independently write stay; record rejected alternatives and why in the LLD's Decisions & Alternatives table, not as asides in body prose.

**Memory vs. intent.** Before saving durable project knowledge to agent or tool memory, test whether it is project *intent* — would a fresh agent, in any tool, next session, need it to build this system correctly? If yes, record it in the arrow (HLD / LLD / EARS / decision doc), which travels and cascades — not in private, per-tool memory, where intent escapes the arrow. Knowledge about the user or how they like to work stays in memory.

### Navigation

| What you need | Where to look |
|---|---|
| High-level design | `docs/high-level-design.md` |
| Design tree (sub-HLDs, LLDs, their specs) | `docs/intent/` — one folder per node |
| EARS specs | beside each design doc as `{node}-specs.md` in the node's folder under `docs/intent/` |
| Decision docs | `docs/decisions/` (project-level) and `docs/intent/<segment>/decisions/` |
| Arrow of intent overlay | `docs/arrows/index.yaml` and per-segment docs in `docs/arrows/` |
| Project decision log | `MANAFOLD.md` — living record of *why* things are the way they are |
| Local dev quickstart | `README.md` |

### Terminology

- **HLD**: High-Level Design — single project-level doc at `docs/high-level-design.md`.
- **LLD**: Low-Level Design — detailed component design doc in `docs/intent/`. The design layer is a recursive tree: the root is the HLD, leaf LLDs own EARS, and a component deep enough to outgrow one doc becomes a sub-HLD (HLD-shaped, owns no EARS) with children beneath it. "HLD" and "LLD" are roles by position; depth-2 (one HLD over flat leaf LLDs) is the default — the case for this project's six segments.
- **EARS**: Easy Approach to Requirements Syntax — structured one-line requirements beside each design doc as `{node}-specs.md` in the node's folder under `docs/intent/`. IDs are path-concatenated — the root-to-leaf path of the owning segment plus a number — so a prefix grep gathers a subtree. Markers: `[x]` implemented, `[ ]` active gap, `[D]` deferred.
- **Arrow**: the unidirectional chain from vision to code (HLD → LLDs → EARS → Tests → Code). Strictly a DAG of intent.
- **Arrow segment**: the territory owned by one leaf LLD — the LLD itself plus the specs, tests, and code that cite its EARS IDs. The boundary is the leaf prefix. Within-segment cascade is free; across-segment cascade pauses.
- **Cascade**: propagating a change downstream through the arrow so adjacent levels stay coherent.

### Segments and their EARS prefixes

| Segment | Prefix | Owns |
|---|---|---|
| `platform-shell` | `PLATFORM` | Go entry point, chi router with per-domain register helpers, config fail-fast, embedded migrations applied at startup, `/health`, the Next.js app-router substrate + same-origin proxy + `lib/api.ts` wrapper |
| `account-access` | `ACCT` | email + password auth (argon2id), server-side sessions, `SessionAuth` / `DevAuth` middleware, anonymous deck drafts + claim-on-sign-in |
| `card-data` | `CARD` | Scryfall bulk-data sync + `sync_runs`, `cards` / `card_prints` / `card_rulings` schema, `GET /api/cards/search` + `/autocomplete`, derived `singleton_limit` / `can_be_commander`, `color_identity` stored verbatim |
| `deck-building` | `DECK` | `decks` + `deck_cards` (qty / board / category), commander & partner assignment, `internal/deckrules` (color identity, singleton, count, banlist, commander shape), `internal/deckstats` (stub), deck CRUD + read-only public share |
| `ai-assist` | `AI` | Anthropic Claude client wrapper, `search_cards` tool over the mirror, suggest-&-explain, deck-health prose, bracket estimate, the validate-every-returned-card gate |
| `import-export` | `PORT` | parse/emit plain-text, MTGA, Moxfield, Archidekt decklists; `imports` audit table; unresolved-name reporting |

`collections` and `social` are deferred; each has a one-paragraph stub LLD only.

### Code annotations

Annotate code and tests with `@spec` comments citing EARS IDs:

```
// @spec DECK-004, DECK-006
```

Place the annotation at the *entry point of the behavior's implementation graph* — the topmost function or module owning the specified behavior, not every helper. When a behavior spans multiple subsystems (UI + API + DB, for example), annotate at the entry point in each subsystem. Tests follow the same rule: annotate the test that directly exercises the spec, not every inner assertion.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
