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
decklist. **M2** added import/export + deterministic deck stats + the Slate &
Signet palette. **M3** added Manafold's own email + password accounts (argon2id),
server-side sessions, per-IP rate limiting on login/register, and anonymous deck
drafts claimable on sign-in; `AnonOrSession` replaces the `DEV_AUTH` stub as the
default auth path (the stub stays available for local/CI). All milestones land on
one branch as a single growing PR.

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

- **2026-09-02** — M2 increment (same branch, one growing PR). Import/export and
  deterministic deck stats, plus the brand palette wire-in and the closure of an
  M1 test gap.
  - **Import / export** (`PORT-001..007`, `DECK-060`): `internal/deckio` — one
    tolerant line grammar for plain-text / MTGA / Moxfield / Archidekt (formats
    differ only in header handling), plus plain-text and MTGA emitters; a pure
    package with no name resolution and no DB. An `imports` audit table (raw
    text + parsed result + unresolved lines + `applied_at`). Endpoints `POST
    /api/decks/{id}/import` (parse + audit, no write), `POST
    /api/decks/{id}/import/{importId}/apply` (writes `deck_cards` for every
    resolved line in one transaction), `GET /api/decks/{id}/export?format=`.
    Name resolution is exact case-insensitive or one face of a split/DFC card →
    whole card; a name matching nothing is reported and stored, never dropped,
    and an unparseable line is reported separately as *rejected*. Ownership
    scoped in the queries → `404`.
  - **Deck stats** (`DECK-051`, `DECK-052`): `internal/deckstats.Analyze` grew
    from the M1 stub to a deterministic analyser — non-land mana curve
    (`0`–`6`, `7+`), colour-pip demand vs colour sources, a functional-category
    roll-up over a known vocabulary + synonym table with free-text passthrough,
    and exported `CategoryTargets` rules-of-thumb bands for Ramp / Card Draw /
    Removal / Board Wipe / Counterspell (Land carries no band — the roll-up only
    counts manually tagged cards, so the real land signal is the separate
    land / non-land split). `GET /api/decks/{id}/stats` echoes the analysis plus
    the targets; the builder page renders a stats panel. LLM prose over these
    numbers stays M5.
  - **Palette — "Slate & Signet"** (captain decision, brand direction A): the
    light + dark semantic-token set and the six `--color-mana-*` WUBRG tokens
    wired into `frontend/src/app/globals.css`, replacing the M1 placeholder
    palette. The "Manafold" text wordmark (display "Mana" + faded "fold" tail)
    is the approved mark; the favicon is a neutral placeholder pending the
    captain's vector artwork (`PLATFORM-024` stays deferred — the mark supplied
    so far is raster).
  - **CARD-006** now carries a dedicated test
    (`internal/cardsync/httpfetch_test.go`): every outbound Scryfall request
    carries the descriptive `User-Agent` + explicit `Accept`, and an HTTP `429`
    triggers exactly one retry after the (test-shortened) back-off.
  - **Deferred out of M2** and recorded in the LLDs' future-decisions:
    `PORT-008` / `CARD-009` (live single-printing fallback — waits on
    printing-selection UI); trigram fuzzy name matching; auto-assigning the
    commander from an imported `Commander` line; the Oracle-text + `oracle_tags`
    auto-categorizer; and the captain's five "bonus" ideas (bootstrapping
    wizard, EDHREC high-synergy, constrained deck prompts, functional-subtype
    grouping, cut suggestions) — design notes only, folded into the relevant
    LLDs.

- **2026-09-03** — M3 increment (same branch, same growing PR). Manafold's own
  email + password accounts, per captain decision D4 (not Google OAuth).
  - **Password hashing** (`internal/passwordhash`, `ACCT-010`, `ACCT-011`,
    `ACCT-018`): argon2id (`m=19456` KiB, `t=2`, `p=1`, 16-byte salt, 32-byte
    key — the OWASP second-choice profile, deliberately low on memory for the
    Render free tier), parameters written into the encoded hash string so they
    can rise later with no schema change. `Verify` also accepts a bcrypt hash
    (`$2a/$2b/$2y`), keeping bcrypt a drop-in fallback and old hashes valid.
  - **Rate limiting** (`internal/ratelimit`, `ACCT-017`): in-process per-IP
    token bucket (capacity 10, +1 token / 6 s), checked before any DB work on
    `POST /api/auth/login` and `POST /api/auth/register`; `429` on an empty
    bucket. The IP key is the first entry of the trusted `X-Forwarded-For`
    suffix — `TRUSTED_PROXY_COUNT` hops from the right (default `1`), else
    `RemoteAddr` — so a forged left-most entry cannot mint a fresh bucket; see
    `account-access` design § Rate limiting. A shared store is only needed past
    one backend instance (LLD open question).
  - **Sessions + endpoints** (`ACCT-003`, `ACCT-012..014`, `ACCT-019..021`):
    server-side `sessions` rows (30-day expiry, revocable), session cookie
    (`HttpOnly`, `Secure`, `SameSite=Lax`). `POST /api/auth/register|login`
    (email trimmed + lowercased; a dummy verify runs for an unknown email so
    timing does not leak account existence; one generic `401` on any login
    failure), `POST /api/auth/logout` (`204`, no-op if the cookie names no live
    session), `GET /api/auth/session` (never `401`), `POST
    /api/auth/claim-drafts` (needs a live session, idempotent).
  - **`AnonOrSession` middleware** (`ACCT-003`, `ACCT-020`): the default
    protected-group auth — session cookie first, then a non-empty `X-Anon-Token`
    header, else `401`; a valid session always wins over a supplied token.
    `DevAuth` stays available behind `DEV_AUTH=true` for local dev and CI (the
    deck integration tests inject identity through `authctx` directly).
  - **Anonymous drafts + claim** (`DECK-040`, `DECK-041`): every
    `deck-building` query now scopes to a polymorphic owner key
    (`decks.user_id = narg(user_id) OR decks.anon_token = narg(anon_token)`, one
    non-null); `CreateDeck` inserts whichever is set; `ClaimAnonDecks`
    reassigns a token's drafts to the signed-in user in one `UPDATE`. The
    frontend mints `manafold_anon` (`crypto.randomUUID()`) in `localStorage`,
    sends it as `X-Anon-Token` on every request, and calls `claim-drafts` on
    login and register.
  - **Frontend**: `(auth)` route group with `/login` and `/register` pages, a
    `lib/auth.ts` helper, and header session state (email + "Sign out" when
    authenticated, "Sign in" / "Create account" when anonymous). The app stays
    fully usable anonymously — that is the point of the draft flow.
  - **Deferred out of M3** and recorded in `account-access`'s future-decisions:
    OAuth providers (`ACCT-030`); email verification + password reset
    (`ACCT-031`, waits on a transactional email sender — `email_verified_at` is
    stored but not enforced); sign-out-everywhere (`ACCT-032`); a shared
    rate-limit store for a multi-instance deploy.
