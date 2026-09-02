---
parent: high-level-design
prefix: PLATFORM
---

# Platform Shell

## Context and Design Philosophy

Everything that is not one specific feature: the Go entry point, HTTP routing and
shared handler infrastructure, the embedded-migration mechanism, `/health`, and
the Next.js app-router substrate with its same-origin API proxy and shared
`lib/api.ts` client. This is the widest segment by surface but the shallowest by
intent — it exists so the other five segments have a runnable, presentable place
to live.

## Backend: Entry Point, Routing, Shared Infra

`cmd/api/main.go` runs a fixed startup sequence and treats every failure in it as
fatal (`log.Fatalf`), with no graceful shutdown:

1. `config.Load()` — reads environment (a `.env` file first for local dev; real
   environment always wins). Fails with a message naming the missing variable if
   a required one is unset.
2. `db.Migrate(cfg.DatabaseURL)` — applies every pending migration under
   `internal/db/migrations`, embedded into the binary via `//go:embed`, *before*
   the connection pool opens or any request is served. The binary that expects a
   schema is the one that applies it, on every deploy, with no separate step for
   anyone to remember.
3. `pgxpool.New(ctx, cfg.DatabaseURL)` — the long-lived pool.
4. `server.New(server.Deps{...})` — builds the chi router.
5. `http.ListenAndServe(":"+cfg.Port, handler)`.

There is deliberately **no `secrets.NewBox` and no PDF renderer** — Manafold has
no bring-your-own-key custody and no document-render pipeline.

`internal/server/server.go` builds the chi router: `middleware.Logger` and
`middleware.Recoverer` globally, then `GET /health` (which pings Postgres, so it
doubles as a DB-connectivity check), then `/api/auth/*` as an unauthenticated
sibling group, then the protected `/api` group. **No CORS middleware** — the
browser only ever reaches the backend through the frontend's same-origin `/api/*`
rewrite (`ACCT-016`), and the session cookie is scoped to the frontend origin.

Route registration is split into **per-domain helper functions from the start** —
`registerCardRoutes(r, h)`, `registerDeckRoutes(r, h)`, later
`registerAuthedRoutes` / `registerAiRoutes` / `registerPortRoutes` — rather than
one flat list. Each helper lives in the segment that owns those routes' handlers
(`internal/api/cards.go`, `internal/api/decks.go`) and is called from
`server.go`. Handlers are methods on one `api.API` struct that bundles every
dependency (`Pool`, `Queries`, and later `AI`), not package globals.

`internal/config/config.go` loads one `Config` struct with explicit `os.Getenv`
per field and fail-fast validation. `config.Load` is the API server's path and
requires both `DATABASE_URL` and `FRONTEND_URL`; `config.LoadCardsync` is the
sync job's path and requires only `DATABASE_URL`, since that process has no HTTP
surface and never needs the frontend origin. Required unless `DEV_AUTH=true`:
nothing in M1 (the real login flow is M3, at which point the argon2id/session
config becomes required unless `DEV_AUTH=true`). `ANTHROPIC_API_KEY` becomes
required at M4. `PORT` defaults to `8080`; `.env.example` documents the same
value — no mismatch.

`internal/api/api.go` + `internal/api/convert.go` are the shared foundation:
the `API` struct, `writeJSON` / `writeError`, and `pgtype` conversion helpers
(`textPtr`, `rawOrNull`, `parseUUID`, …). `internal/db/generated/` holds the sqlc output
(`db.go`, `models.go`, `querier.go`, `*.sql.go`) — referenced by every backend
segment, never hand-edited. sqlc config: v2, `engine: postgresql`,
`sql_package: pgx/v5`, `emit_interface: true`, `emit_json_tags: true`, `jsonb` →
`encoding/json.RawMessage`, schema read from `internal/db/migrations`,
`out: internal/db/generated`.

## Backend: Standalone Binaries

`cmd/cardsync/main.go` is a second `main` package in the same module and Docker
image: it loads its DB-only config (`config.LoadCardsync`), applies every pending
embedded migration, opens a pool, and runs `cardsync.Run` (see `card-data`).
It exists so the daily sync is a plain process with no HTTP surface, invokable as
a Render cron (`dockerCommand: /app/cardsync`) and as `go run ./cmd/cardsync`
for local seeding. The Dockerfile builds both `cmd/api` and `cmd/cardsync`.

## Frontend: App-Router Substrate

`frontend/src/app/layout.tsx` is the root layout (fonts, `<html>`/`<body>`).
`globals.css` holds Tailwind v4 CSS-first `@theme` tokens: a raw palette plus
**semantic aliases** (`--color-background`, `-surface`, `-foreground`,
`-primary`, `-accent`, `-border`) declared as **literal values, not
cross-references** — Tailwind v4 resolves `@theme` cross-references at build time
and bakes them into utilities, so a runtime override of a cross-referenced token
is a no-op. A `.workspace` class (plain CSS, outside `@theme`) overrides those
same custom properties for the in-app light theme.

Route groups: `(marketing)/` for the public landing page, `(app)/` for the
builder shell (`decks/page.tsx`, `decks/[id]/page.tsx`). `src/proxy.ts` is the
Next 16 "Proxy" (renamed from Middleware) — a deliberate pass-through with a
matcher for `(app)` routes; it does **not** gate on session-cookie presence
(that check cannot tell a valid session from an expired one — route protection
is the client-side 401 handler's job, `ACCT-015`).

`frontend/next.config.ts` `rewrites()` maps `/api/:path*` →
`${BACKEND_ORIGIN}/api/:path*` (a server-side-only env var). The browser only
ever talks to the frontend origin (`ACCT-016`).

`frontend/src/lib/api.ts` is the single fetch wrapper every page calls: a generic
`request<T>(path, init?)` with `credentials: "include"` and a JSON
`Content-Type` default, a typed `ApiError { status, message }`, and centralized
`401` handling (`window.location.href = "/"`, `ACCT-015`). One exported `api`
object with one method per endpoint (`exportDeck` bypasses `request` because it
returns `text/plain`). `frontend/src/lib/deck.ts` and
`frontend/src/lib/deckstats.ts` hold small pure view helpers.

### Palette and wordmark

The theme is brand direction **"Slate & Signet"** (adopted 2026-09-02): neutral
cool-slate surfaces framing a single violet primary (`#5B3FD4` light /
`#6E5CE0` dark) deliberately off the blue/green wavelengths the WUBRG mana
colours occupy, so a button never reads as a mana pip. `globals.css` carries the
full semantic set (background / surface / surface-2 / foreground / muted /
primary / primary-ink / accent / accent-2 / border / success / warning / danger
/ info) as literal light values in `@theme`, re-declared under `.workspace` for
the in-app light theme, plus six `--color-mana-{w,u,b,r,g,c}` tokens (W also
carries `--color-mana-w-ring`) that are the loud category colours the brand
frame is quiet around.

The **"Manafold" wordmark** — display face (Space Grotesk) for the "Mana" stem,
a lighter faded weight for the "fold" tail — is the approved plain-text mark
(`frontend/src/components/Wordmark.tsx`, `.wordmark` styles in `globals.css`).
The favicon is a neutral placeholder; the captain supplies the final mark, and a
vector version of it is still wanted (`PLATFORM-024` stays deferred — the mark
delivered so far is raster).

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Route registration shape | Per-domain `registerXRoutes(r, h)` helpers from the start | One flat `r.Get/Post/...` list in `server.go` (the sibling project's shape, flagged there as worth splitting past ~20 routes) | Manafold reaches ~20 routes within M1–M2; starting split avoids a later churny refactor and keeps each segment's routes beside its handlers. |
| Migration application | Embedded via `//go:embed`, applied by the API binary at startup before the pool opens | A manual `migrate ... up` deploy step; a Render release-phase command | A manual step depends on a human remembering it on every migration-bearing deploy; the release-phase command needs a paid Render plan. In-binary migration runs identically on every target. |
| `cmd/cardsync` as a separate binary | Standalone `main` package in the same module/image, run as a Render cron | An in-process goroutine ticker in the API binary; an external scheduler invoking an HTTP endpoint | A separate process has no HTTP surface to secure and does not couple sync to API uptime or duplicate work if the API scales past one instance. An in-process ticker is acceptable only for a single free-tier instance — noted as a fallback in `card-data`'s Open Questions. |
| Startup failure handling | Fatal on any startup error; no graceful shutdown | Signal handling + `http.Server.Shutdown` | Acceptable for the current stage; a gap under Render's Docker runtime, tracked below. |
| `PORT` default | `8080`, matching `.env.example` | An `8081` code-vs-docs split (the sibling project's known mismatch) | No reason to inherit a documented inconsistency into a fresh project. |
| Frontend → backend transport | Same-origin `/api/*` rewrite in `next.config.ts` | Direct cross-origin calls to the backend + CORS | A first-party session cookie is not subject to third-party-cookie blocking; no CORS surface to get wrong. |

## Open Questions & Future Decisions

### Deferred
1. **Graceful shutdown** — signal handling so in-flight requests finish on
   redeploy. Acceptable to skip while the backend is a single free-tier
   instance; revisit before any multi-instance deploy.
2. **Real vector logo** (`PLATFORM-024`) — the "Slate & Signet" text wordmark
   ships for v1. The captain has delivered a raster mark (icon + lockup PNGs on
   a dark ground); a clean vector version, and its use as the favicon /
   app-icon, are still outstanding.
3. **Shared visual-polish primitives** (completion rings, status chips, radial
   glow as a system) — carried in spirit for M1; formalise if the UI grows.

### Gaps
4. **`/health` does not distinguish "migrations pending" from "DB reachable"** —
   it only pings. A migration that failed at startup already exits the process,
   so this is informational only.

## References

- Code: `backend/cmd/api/main.go`, `backend/cmd/cardsync/main.go`,
  `backend/internal/server/server.go`, `backend/internal/config/config.go`,
  `backend/internal/api/api.go`, `backend/internal/api/convert.go`,
  `backend/internal/db/migrate.go`, `backend/internal/db/generated/*`
- Hosting: `backend/Dockerfile`, `render.yaml`, `.github/workflows/ci.yml`
- Frontend: `frontend/src/app/layout.tsx`, `frontend/src/app/globals.css`,
  `frontend/src/components/Wordmark.tsx`, `frontend/src/proxy.ts`,
  `frontend/next.config.ts`, `frontend/src/lib/api.ts`,
  `frontend/src/lib/deck.ts`, `frontend/src/lib/deckstats.ts`
- Cross-segment: every `/api/*` route runs under `account-access`'s auth
  middleware (`ACCT-001` / `ACCT-012`); `card-data` and `deck-building` register
  their routes through this segment's helpers.
