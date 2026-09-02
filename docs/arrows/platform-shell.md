# Arrow: platform-shell

Go entry point, HTTP routing with per-domain register helpers, shared handler
infrastructure, the embedded-migration mechanism, `/health`, and the Next.js
app-router substrate with its same-origin API proxy and shared `lib/api.ts`.

## Status

**MAPPED** — greenfield, authored with the M1 slice (2026-09-02). Not yet
audited as a full segment.

## References

### HLD
- docs/high-level-design.md (System Design; Key Design Decisions — monorepo / migrations / transport)

### LLD
- docs/intent/platform-shell/platform-shell-design.md

### EARS
- docs/intent/platform-shell/platform-shell-specs.md (PLATFORM-001..007, PLATFORM-020..024)

### Tests
- backend/internal/db/migrate_test.go (`TestMigrate_FromEmptySchema_ReachesLatestVersion`) — PLATFORM-001
- backend/internal/server/server_test.go (`TestHealth_*`, `TestProtectedRoute_*`, `TestPublicDeckRoute_*`) — PLATFORM-002, PLATFORM-005
- backend/internal/config/config_test.go — PLATFORM-003
- PLATFORM-004 (no CORS), PLATFORM-006 (cardsync binary in the image), and the
  frontend-config specs PLATFORM-020..023 are structural invariants verified by
  `go build ./...` / the Dockerfile / `npm run lint` + `npm run build`, not by a
  dedicated `@spec`-annotated test. PLATFORM-023's `.workspace` runtime override
  now carries the "Slate & Signet" semantic-token values.

### Code
- backend/cmd/api/main.go, backend/cmd/cardsync/main.go
- backend/internal/server/server.go (`New`, `RegisterCardRoutes`, `RegisterDeckRoutes`, `RegisterImportRoutes`, `healthHandler`)
- backend/internal/config/config.go
- backend/internal/api/api.go, backend/internal/api/convert.go
- backend/internal/db/migrate.go, backend/internal/db/generated/* (sqlc)
- frontend/src/app/layout.tsx, frontend/src/app/globals.css (Slate & Signet tokens + `.wordmark`), frontend/src/components/Wordmark.tsx, frontend/src/proxy.ts
- frontend/next.config.ts, frontend/src/lib/api.ts, frontend/src/lib/deck.ts

## Architecture

**Purpose:** make the other five segments runnable and presentable — routing,
shared plumbing, migrations, the frontend shell.

**Key components:**
1. Go entry point + router — `main.go`, `server.go`; per-domain `registerXRoutes`
   helpers; no CORS (same-origin proxy instead); no graceful shutdown.
2. `cmd/cardsync` — the daily sync as a separate process in the same image.
3. Embedded migrations applied at startup before the pool opens.
4. Shared handler infra — `api.go` / `convert.go` / generated `db` package.
5. Frontend substrate — root layout, Tailwind v4 semantic tokens with a
   `.workspace` runtime override, the Next 16 Proxy pass-through, the
   same-origin `/api/*` rewrite, the `lib/api.ts` client.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Backend shell | PLATFORM-001..006 | 6 | 0 | 0 |
| Graceful shutdown | PLATFORM-007 | 0 | 0 | 1 |
| Frontend substrate | PLATFORM-020..023 | 4 | 0 | 0 |
| Brand artwork | PLATFORM-024 | 0 | 1 | 0 |

**Summary:** 10 of 12 implemented; 1 gap (graceful shutdown); 1 deferred (logo artwork).

## Key Findings

1. Route registration is split into per-domain helpers from the start
   (PLATFORM-005), unlike the sibling project's flat list — Manafold reaches
   ~20 routes inside M1–M2.
2. `PORT` default is `8080` in both `config.go` and `.env.example` — no mismatch
   carried over.

## Work Required

### Must Fix
(none)

### Should Fix
(none)

### Nice to Have
1. Graceful shutdown (PLATFORM-007) before any multi-instance deploy.
