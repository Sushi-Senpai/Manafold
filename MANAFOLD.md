# Manafold

Living reference doc for the Manafold project. Read this at the start of any
session touching this project; update it whenever a real decision gets made or
changed. This is the single source of truth for *why* things are the way they
are — keep it in sync as the project evolves rather than letting decisions live
only in chat history. The engineering design lives in the LID arrow
(`docs/high-level-design.md` + `docs/intent/`); this doc is the product-level
decision log and status narrative that sits above it, the way `WAYSTONE.md` does
for Waystone.

## Where We Left Off (read this first)

**Project scaffolded from an empty repo (2026-09-02).** The LID bootstrap
(`AGENTS.md`, HLD, six leaf LLDs + EARS, `docs/arrows/`), the Waystone-mirrored
Go/Next.js repo skeleton, and the M1 vertical slice were stood up in one
scaffold task. M1 is: create a deck → set a commander → search & add cards with
live color-identity + singleton + count + banlist validation → view the
decklist. M1 runs on the `DEV_AUTH` stub only; the real email + password login
flow is designed in `account-access` now and implemented at M3.

## Vision

Manafold is a Commander-first Magic: The Gathering deckbuilder. Commander
deckbuilding has a high rules-and-knowledge floor — 100-card singleton, colour
identity, the banlist, the ~10 ramp / ~10 draw / ~5 removal / ~3 wipes / ~37
lands template, bracket etiquette — and the dominant tools (Moxfield, Archidekt)
are generic list editors that do not enforce Commander legality live or help a
player reason about synergy and power level. Manafold does both: legality is a
structural feature validated against Scryfall's own fields, deterministic
analysis runs against known heuristics, and an AI layer curates and *explains*
on top of decklist-co-occurrence statistics — never inventing a card or
suggesting one outside the commander's colour identity.

This is a real full-stack application (Go/chi/pgx, Next.js, LLM tool-use, LID
from inception), not a monetized product.

## Product Decisions & Rationale (captain, 2026-09-02)

The captain reviewed the setup research report and answered all six gating
decisions "all recommendations except no Google auth for the first pass, just do
our own log-ins."

| # | Decision | Choice | Rationale |
|---|---|---|---|
| D1 | AI provider & key model | Anthropic Claude via `github.com/anthropics/anthropic-sdk-go`; developer-held key, server-side per-user quotas + per-user cost caps + a global spend ceiling. **Not BYOK.** | AI assistance is a core, always-on feature of the product's pitch, not an optional power-user tool; asking every visitor for an API key would gut adoption. A BYOK escape hatch may be added later. Deliberate switch away from Waystone's OpenAI code. |
| D2 | Card data strategy | Mirror the Scryfall bulk data (Oracle Cards + Default Cards) into Manafold's own Postgres, refreshed by a daily sync job; serve all search / autocomplete / card-detail traffic from the mirror. Live Scryfall calls only in the sync job and a rare "printing not in the mirror yet" fallback on card add. | Scryfall's own docs require bulk data for high-volume use; Manafold needs SQL joins between cards and decks for its core validation; sub-10 ms search with full index control; no third-party dependency on the request hot path. |
| D3 | v1 product scope | Deckbuilding + a read-only public deck share URL. Deferred past v1: collection tracking, social feed / comments / following, playtester / goldfish, deck version diffing, folders, price-history / affiliate. | Keeps v1 focused on the differentiator (live Commander legality) and its smallest useful surface. |
| D4 | Auth model — **diverges from the research recommendation** | v1 uses Manafold's **own email + password logins**, not Google OAuth. No Google OAuth client for the first pass. Passwords hashed with argon2id (preferred) or bcrypt. Server-side sessions as in Waystone (session cookie + `sessions` table). `DEV_AUTH=true` dev/CI stub kept. Anonymous deck drafts claimable on sign-in. The design must not preclude adding OAuth providers later. M1 uses only the `DEV_AUTH` stub; the real login flow is implemented at M3. | The captain's explicit call for the first pass. |
| D5 | Hosting & pipeline | Vercel (frontend) + Render Docker free tier (Go backend) + Neon serverless Postgres, mirroring Waystone. `.github/workflows/ci.yml` (Postgres service container) and `.no-mistakes.yaml` wired from day one. Card-sync job as a Render cron. GitHub remote `github.com/Sushi-Senpai/Manafold` (public) already exists and is registered. | Matches Waystone's proven topology; retrofitting the arrow/tests discipline later is the expensive path. |
| D6 | Mobile support | Responsive web only for v1. No mobile-tuned builder, no native app. | The builder is a desktop-first workflow. |

### Smaller calls adopted as-is (research report §7)

- Show Scryfall USD/EUR prices from the mirror; **no affiliate links** in v1.
- Deck organization: **Archidekt-style functional categories**
  (`deck_cards.category` free-text + a server-side auto-categorizer stub), not
  Moxfield-style light tags. The functional groups (Removal, Counterspell, Ramp,
  Card Draw, Board Wipe, Land, …) are first-class in the category model and,
  later, the builder UI (captain bonus feature #4).
- Recommendation engine (later): LLM + functional tags + `edhrec_rank` for v1,
  extended with **EDHREC high-synergy data** (captain bonus feature #2 — EDHREC
  has no official public API; the `ai-assist` LLD records the data-access and
  ToS question as open), and an own public-decklist co-occurrence corpus built
  over time.
- Third-party data: Scryfall (required) + Commander Spellbook's open combo
  database for later combo detection; nothing else.
- Brand/design: a plain text wordmark is fine for M1 (Waystone shipped that
  way); a real vector logo is a later pass.

## Later features to design for, not build (captain, 2026-09-02)

These are folded into the relevant LLDs' *Open Questions & Future Decisions* and
the HLD roadmap so the architecture does not preclude them. **Not M1 scope.**

1. **Deck-bootstrapping wizard** — commander → auto mana base from the colour
   identity → a wizard adding the most powerful/common cards for those colours.
   (`deck-building` + `ai-assist`; related to research milestone M8.)
2. **EDHREC high-synergy cards** as a recommendation input, not just
   `edhrec_rank`. (`ai-assist`; data-access + ToS is an open question.)
3. **Constrained deck generation beyond brackets** — budget cap, "no infinite
   combos", "no tutors", tribe/theme lock, house rules. (`ai-assist`.)
4. **Functional-subtype grouping** first-class in the category model and builder
   UI. (`deck-building`; reinforces the adopted functional-category decision.)
5. **Cut suggestions / replacement discussion** — surface underperforming or
   low-synergy cards as cut candidates with suggested replacements, low-friction
   swap UX. (`ai-assist` deck-health + `deck-building` swap UX.)

## Tech Stack

Mirrors Waystone. See `docs/high-level-design.md` § Key Design Decisions and
`backend/`/`frontend/` for specifics.

| Layer | Choice |
|---|---|
| Frontend | Next.js 16 (App Router) + TypeScript + Tailwind v4 + Yarn |
| Backend | Go + [chi](https://github.com/go-chi/chi) router |
| DB access | [pgx](https://github.com/jackc/pgx) + [sqlc](https://sqlc.dev/) codegen (write real SQL, generate type-safe Go) |
| Migrations | [golang-migrate](https://github.com/golang-migrate/migrate), embedded via `//go:embed` and applied by the API binary at startup before the pool opens |
| Database | PostgreSQL 16 (Neon serverless in prod, direct non-pooled connection string) |
| Card data | Mirror of the Scryfall bulk-data exports, synced daily |
| AI provider | Anthropic Claude, `github.com/anthropics/anthropic-sdk-go`, developer-held key (no AI code ships in M1) |
| Auth | Manafold email + password (argon2id) + server-side sessions + `DEV_AUTH` stub (M3; M1 is stub-only) |
| Hosting | Vercel (frontend) + Render Docker (backend) + Neon (Postgres); card-sync as a Render cron |
| CI | GitHub Actions (`.github/workflows/ci.yml`) with a Postgres service container, plus the shared no-mistakes pipeline (`.no-mistakes.yaml` sets `ci: { no_ci: true }`) |

## Repo Structure

Monorepo, single git repo at `manafold/` root. See
`docs/high-level-design.md` § System Design and `docs/arrows/index.yaml` for the
segment map, and `README.md` for the local-dev quickstart.

```
manafold/
├── MANAFOLD.md               — this doc
├── AGENTS.md  CLAUDE.md      — LID mode declaration + navigation
├── README.md
├── .no-mistakes.yaml
├── docker-compose.yml        — local postgres:16-alpine
├── render.yaml               — Render backend web service + card-sync cron
├── .github/workflows/ci.yml
├── docs/
│   ├── high-level-design.md
│   ├── arrows/{index.yaml, <segment>.md}
│   └── intent/<segment>/{<segment>-design.md, <segment>-specs.md}
├── backend/                  — Go API (cmd/api, cmd/cardsync, internal/*)
└── frontend/                 — Next.js app-router
```

## Do NOT carry over from Waystone

Its resume-tailoring product, the resume-render/PDF pipeline (`internal/pdf`,
`internal/resumerender`, the Vercel render endpoint), the `secrets` AES box (no
BYOK here), and the `career-profile` / `job-analysis` / `application-tracking`
domains. Manafold takes the *structure* — router, config, db layer, migrations,
auth-middleware shape, sessions, CI, same-origin proxy — not the resume product.

## Status Log

- **2026-09-02** — Scaffold task. Stood up from an empty repo: the LID bootstrap
  (`AGENTS.md` Full mode, `docs/high-level-design.md` with elicited tenets and a
  six-segment dependency diagram, six leaf LLDs + EARS specs, two deferred
  stubs, `docs/arrows/` overlay), the Waystone-mirrored repo skeleton (Go
  chi/pgx/sqlc/golang-migrate backend, Next.js app-router frontend with the
  same-origin proxy, `docker-compose.yml`, `.github/workflows/ci.yml`,
  `.no-mistakes.yaml`, `render.yaml`), and the M1 vertical slice: `cmd/cardsync`
  ingesting the Scryfall bulk exports into `cards` + `card_prints` +
  `card_rulings` + `sync_runs` with derived `singleton_limit` /
  `can_be_commander` and `color_identity` stored verbatim; `GET
  /api/cards/search` + `/api/cards/autocomplete` over the mirror;
  `decks` + `deck_cards` with `POST /api/decks`, `GET /api/decks/{id}`, `PUT
  /api/decks/{id}/commander`, `POST`/`DELETE /api/decks/{id}/cards`, `GET
  /api/decks/{id}/validation`; `internal/deckrules` (colour identity ⊆ deck
  identity, singleton respecting `singleton_limit` and basics, `main`+`command`
  count vs 100, banlist via `legalities->>'commander'` + a manual-override
  table, commander shape + partner pairing); ownership enforced in-query
  (`404` on a deck you do not own); a read-only public deck view; and the
  `/decks` list + `/decks/[id]` builder page. Tests-first with `@spec`
  annotations: `internal/deckrules` table-driven unit tests, `internal/api` deck
  integration tests against CI Postgres, `internal/cardsync` fixture-JSONL
  ingestion test, `internal/db` `migrate_test.go`. Real login UI/flow,
  import/export, deck stats beyond a stub, any AI call, and printing-selection
  UI are explicitly out of M1.
