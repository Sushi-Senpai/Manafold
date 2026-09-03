# Arrow: account-access

Authentication and the identity seam every `/api/*` request runs through. Email
+ password (argon2id) + server-side sessions + per-IP rate limiting + anonymous
drafts with claim-on-sign-in landed at M3; `DevAuth` remains an opt-in local/CI
stub.

## Status

**MAPPED** — LLD + specs authored 2026-09-02; email + password flow implemented
2026-09-03 (M3). OAuth providers, email verification / password reset, and
sign-out-everywhere are deferred.

## References

### HLD
- docs/high-level-design.md (Key Design Decisions — Auth)

### LLD
- docs/intent/account-access/account-access-design.md

### EARS
- docs/intent/account-access/account-access-specs.md (ACCT-001..003,
  ACCT-010..021, ACCT-030..032)

### Tests
- backend/internal/server/server_test.go (`TestProtectedRoute_*`) — ACCT-001,
  ACCT-002, ACCT-003
- backend/internal/passwordhash/passwordhash_test.go — ACCT-010 (hashing),
  ACCT-011 (verify semantics)
- backend/internal/ratelimit/ratelimit_test.go — ACCT-017
- backend/internal/api/auth_test.go — ACCT-003, ACCT-010..014, ACCT-017,
  ACCT-019..021 (against CI Postgres)

### Code
- backend/internal/middleware/session.go (`AnonOrSession` — ACCT-003, ACCT-020)
- backend/internal/middleware/devauth.go (ACCT-001)
- backend/internal/authctx/context.go (ACCT-002)
- backend/internal/sessioncookie/cookie.go (ACCT-013)
- backend/internal/passwordhash/passwordhash.go (ACCT-010, ACCT-011, ACCT-018)
- backend/internal/ratelimit/ratelimit.go (ACCT-017)
- backend/internal/api/auth.go (`/api/auth/*` handlers — ACCT-010..014,
  ACCT-019, ACCT-021)
- backend/internal/db/queries/{users,sessions}.sql; decks.sql `ClaimAnonDecks`
- frontend/src/lib/api.ts (401 redirect — ACCT-015; anon token; auth calls),
  frontend/src/lib/auth.ts, frontend/src/app/(auth)/{login,register}/page.tsx,
  frontend/next.config.ts (same-origin proxy — ACCT-016), frontend/src/proxy.ts
  (pass-through)

## Architecture

**Purpose:** one identity seam, failing closed.

**Key components:**
1. `authctx` — `WithUserID` / `UserID` and `WithAnonToken` / `AnonToken`; the
   accessors every handler uses (ACCT-002).
2. `AnonOrSession` — session cookie → `GetSession` (filters `expires_at`) →
   `authctx.WithUserID`; else non-empty `X-Anon-Token` → `authctx.WithAnonToken`;
   else `401`. Session wins over a supplied token (ACCT-003, ACCT-020).
3. `DevAuth` — fixed `dev@manafold.local`, active only when `DEV_AUTH=true`
   (ACCT-001).
4. `internal/passwordhash` — argon2id (`m=19456,t=2,p=1`, params in the encoded
   hash), bcrypt accepted on verify (ACCT-010, ACCT-011, ACCT-018).
5. `internal/ratelimit` — in-process per-IP token bucket (cap 10, +1 / 6 s) on
   login + register, checked before any DB work (ACCT-017).
6. `/api/auth/register|login|logout|session|claim-drafts` — email trimmed +
   lowercased (ACCT-019); generic `401` on login failure with a dummy verify for
   an unknown email (ACCT-011); `session` never `401` (ACCT-014); `claim-drafts`
   needs a live session, idempotent (ACCT-021).
7. Same-origin proxy + client-side 401 redirect (shared with `platform-shell`).
8. Anonymous drafts — `X-Anon-Token` header, `decks.anon_token`, `ClaimAnonDecks`
   (ACCT-020, DECK-040/041).

## Spec Coverage

| Category | Spec IDs | Implemented | Deferred | Gaps |
|---|---|---|---|---|
| Identity middleware | ACCT-001..003, ACCT-015..016 | 5 | 0 | 0 |
| Email + password | ACCT-010..014, ACCT-017..019 | 8 | 0 | 0 |
| Anonymous drafts | ACCT-020..021 | 2 | 0 | 0 |
| OAuth / verification / sign-out-everywhere | ACCT-030..032 | 0 | 3 | 0 |

**Summary:** 15 of 18 implemented; 0 gaps; 3 deferred (post-v1).

## Key Findings

1. v1 diverges from the research recommendation: Manafold's own email + password,
   not Google OAuth (captain decision D4).
2. `password_hash` nullable + a provider-agnostic session model keep OAuth
   addable later without migrating existing accounts (ACCT-018).
3. Deck ownership is a polymorphic owner key (`user_id` OR `anon_token`) scoped
   in every `deck-building` query; `ClaimAnonDecks` transfers a token's drafts in
   one `UPDATE` (DECK-040/041).
4. Email verification is stored but never enforced — an unverified account can
   log in (deferred, waits on a transactional email sender).

## Work Required

### Must Fix
(none)

### Should Fix
(none)

### Nice to Have
1. OAuth providers (ACCT-030) post-v1.
2. Email verification + password reset (ACCT-031) once an email sender exists.
3. Shared rate-limit store before a multi-instance deploy.
