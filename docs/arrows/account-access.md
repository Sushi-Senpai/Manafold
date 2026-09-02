# Arrow: account-access

Authentication and the identity seam every `/api/*` request runs through. M1 is
the `DevAuth` stub only; email + password (argon2id) + server-side sessions +
anonymous drafts arrive at M3.

## Status

**DRAFT** — LLD + specs authored 2026-09-02; only `DevAuth` and `authctx` are
implemented (M1). The email + password flow is designed, not built.

## References

### HLD
- docs/high-level-design.md (Key Design Decisions — Auth)

### LLD
- docs/intent/account-access/account-access-design.md

### EARS
- docs/intent/account-access/account-access-specs.md (ACCT-001..003, ACCT-010..021, ACCT-030..032)

### Tests
- backend/internal/server/server_test.go (`TestProtectedRoute_*`) — ACCT-001, ACCT-002

### Code
- backend/internal/middleware/devauth.go (ACCT-001)
- backend/internal/authctx/context.go (ACCT-002)
- frontend/src/lib/api.ts (401 redirect — ACCT-015), frontend/next.config.ts (same-origin proxy — ACCT-016), frontend/src/proxy.ts (pass-through)
- backend/internal/middleware/session.go, backend/internal/sessioncookie/cookie.go, backend/internal/db/queries/{users,sessions}.sql — ship in M1 so the `SessionAuth` path compiles and is test-covered; unused while `DEV_AUTH=true`
- (M3) backend/internal/api/auth.go — the `/api/auth/*` register + login/logout handlers

## Architecture

**Purpose:** one identity seam, failing closed.

**Key components:**
1. `DevAuth` (M1) — fixed `dev-local-user`, active only when `DEV_AUTH=true`.
2. `authctx.UserID(ctx)` — the one accessor every handler uses.
3. `SessionAuth` (M3) — session cookie → `GetSession` (filters `expires_at`) →
   `authctx`; fails closed.
4. Email + password (M3) — argon2id (params in the encoded hash; bcrypt
   fallback), `/api/auth/register|login|logout|session`, per-IP rate limiting,
   generic login-failure response.
5. Same-origin proxy + client-side 401 redirect (shared with `platform-shell`).
6. Anonymous drafts (M3) — `X-Anon-Token` header, `decks.anon_token`,
   `POST /api/auth/claim-drafts`.

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Identity middleware | ACCT-001..003, ACCT-015..016 | 4 | 0 | 1 (ACCT-003, M3) |
| Email + password | ACCT-010..018 | 0 | 0 | 8 (M3) |
| Anonymous drafts | ACCT-020..021 | 0 | 0 | 2 (M3) |
| OAuth / verification / sign-out-everywhere | ACCT-030..032 | 0 | 3 | 0 |

**Summary:** 4 of 17 implemented; 11 gaps (M3); 3 deferred (post-v1).

## Key Findings

1. v1 diverges from the research recommendation: Manafold's own email + password,
   not Google OAuth (captain decision D4).
2. `password_hash` nullable + a provider-agnostic session model keep OAuth
   addable later without migrating existing accounts (ACCT-018).
3. M1 has no real accounts — every deck is owned by `dev-local-user`;
   multi-user ownership is exercised in tests by inserting a second user row.

## Work Required

### Must Fix
(none — M1 scope is `DevAuth` only)

### Should Fix
1. Implement ACCT-003 and ACCT-010..021 at M3.

### Nice to Have
1. OAuth providers (ACCT-030) post-v1.
