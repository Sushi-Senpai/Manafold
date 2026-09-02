---
parent: high-level-design
prefix: ACCT
---

# Account & Access

## Context and Design Philosophy

Authentication and the identity seam every `/api/*` request runs through.
Manafold v1 uses its **own email + password logins** — not a third-party OAuth
provider — with server-side sessions and a `DEV_AUTH` stub for local work and CI.
The design must not preclude adding OAuth providers later, but v1 ships only
email + password.

Every handler reads the caller's identity through one function,
`authctx.UserID(ctx)`, set by whichever auth middleware ran. The middleware
**fails closed** — on any failure it responds `401` and does not call the
downstream handler, so a handler never has to check the ok-bool.

**M1 uses the `DEV_AUTH` stub only.** The email + password flow below is designed
now and implemented at M3.

## Identity Middleware

`internal/middleware`:

- **`DevAuth(queries)`** — active only when `DEV_AUTH=true` (off by default).
  Upserts one fixed user (`dev-local-user`, `dev@manafold.local`) and attaches
  its ID to every request. No cookie, no token. Lets a contributor run the
  builder with no account infrastructure.
- **`SessionAuth(queries)`** (M3) — reads the session cookie, calls
  `GetSession` (whose SQL filters `expires_at > now()`, so "no rows" covers
  missing *and* expired), attaches `authctx.WithUserID(ctx, session.UserID)`,
  fails closed on any failure.

`server.New` selects one: `if d.DevAuth { r.Use(DevAuth) } else {
r.Use(SessionAuth) }`. `/api/auth/*` and `/health` and `/api/public/*` are the
only routes outside the protected group.

## Email + Password (M3)

### Schema (migration `add_credentials`, M3)

- `users` gains `email` (already unique) and `password_hash` text null (null for
  a future OAuth-only user), `email_verified_at` timestamptz null.
- `sessions` — `id uuid pk`, `user_id uuid fk → users on delete cascade`,
  `expires_at timestamptz not null`, `created_at timestamptz not null`. Index on
  `user_id`. No `updated_at` (a session is created, read, deleted/expired, never
  edited).

### Password hashing

**argon2id** via `golang.org/x/crypto/argon2` (`IDKey`), parameters stored in
the encoded hash string (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`), so they
can be raised later without a schema change. A per-password 16-byte random salt.
bcrypt (`golang.org/x/crypto/bcrypt`) is the accepted fallback if argon2id
tuning proves troublesome on the Render free tier's memory ceiling; the encoded
prefix makes the two distinguishable at verify time.

### Endpoints (`/api/auth/*`, unauthenticated)

| Endpoint | Method | Body | Behaviour |
|---|---|---|---|
| `/api/auth/register` | POST | `{ email, password }` | validate email shape + password length (≥ 10); reject `409` if the email exists; hash; create the user; create a session; set the cookie |
| `/api/auth/login` | POST | `{ email, password }` | constant-time-compare the hash; on success create a session + set the cookie; on failure `401` with a generic message (no "user not found" vs "wrong password" distinction) |
| `/api/auth/logout` | POST | — | delete the session row, clear the cookie; CSRF-open by design (forced logout only, no data impact) |
| `/api/auth/session` | GET | — | `{ authenticated: bool, email? }`; used by the landing page; does not 401 |

### Session cookie

`internal/sessioncookie` owns the name (`manafold_session`) and flags
(`HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, 30-day expiry matching the
session row). `SameSite=Lax` is sufficient because the same-origin proxy
(`ACCT-016`) makes it a first-party cookie of the frontend origin.

### Rate limiting

`/api/auth/login` and `/api/auth/register` are rate-limited per client IP (a
small in-process token bucket in M3; a shared store if the backend ever scales
past one instance) to blunt credential stuffing. `429` on trip.

## Same-Origin Proxy

`frontend/next.config.ts` rewrites `/api/:path*` to
`${BACKEND_ORIGIN}/api/:path*` (server-side-only env var). The browser only ever
talks to the frontend's own domain, so the session cookie is first-party, not a
third-party cookie subject to browser blocking. This is the same mechanism the
sibling project uses and Manafold copies it verbatim.

## Client-Side Route Protection

The frontend Proxy (`src/proxy.ts`) is a pass-through — a session-cookie
presence check there cannot distinguish a valid session from an expired or
revoked one. Instead, `lib/api.ts` redirects to `/` on any `401` response
(`ACCT-015`); pages never write their own "am I logged out" logic. The
trade-off is a brief flash of a protected page's shell before the redirect;
no protected *data* renders, since every API call still 401s without a valid
session.

## Anonymous Deck Drafts (M3)

A first-time visitor can build a deck without signing in. The frontend mints an
opaque `anon_token` (stored in `localStorage`) and sends it as an
`X-Anon-Token` header. When no session is present but the header is, a thin
`AnonOrSession` middleware resolves the caller to that token; `deck-building`'s
queries accept `anon_token` in place of `user_id` (the `decks` CHECK enforces
exactly one). On sign-in, the frontend calls `POST /api/auth/claim-drafts` with
the token; the backend reassigns every `decks` row owned by that token to the
new `user_id` and nulls the token, in one transaction.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Auth mechanism (v1) | Manafold email + password (argon2id) + server-side sessions | Google OAuth (the research recommendation); passwordless magic links; a hosted identity provider | The captain's explicit call for the first pass. The `password_hash`-nullable `users` row and the provider-agnostic session model keep OAuth addable later without a migration of existing accounts. |
| Password KDF | argon2id, parameters in the encoded hash string; bcrypt as a documented fallback | bcrypt only; scrypt; PBKDF2 | argon2id is the current best-practice memory-hard KDF; encoding the parameters lets them rise later with no schema change. bcrypt stays a fallback because Render's free tier caps container memory and aggressive argon2 `m` can be tight. |
| Session vs JWT | Server-side session rows | Stateless signed JWTs | A session row is revocable immediately (logout, "sign out everywhere", a compromised account); a JWT is valid until expiry. |
| Login failure response | One generic `401` for both unknown-email and wrong-password | Distinct messages | Distinct messages are a user-enumeration oracle. |
| Anonymous drafts | `decks.user_id` nullable + `anon_token` + a claim endpoint; CHECK enforces exactly one owner | Require sign-in before creating a deck; store anon decks only in `localStorage` | Lowering the barrier matters for a hobby tool; a server-side anon row means the draft survives a browser change once claimed and is the same `decks` row throughout. |
| Dev auth | Keep a `DEV_AUTH=true` fixed-user stub, off by default | Only ever run real auth locally | Contributors and CI need to exercise every other segment without standing up email delivery or a session store; M1 depends on it entirely. |

## Open Questions & Future Decisions

### Deferred
1. **OAuth providers** (Google and others) — a `user_identities` table
   (`user_id`, `provider`, `provider_sub`), the authorization-code flow, and
   account linking. The v1 schema does not block it. Post-v1.
2. **Email verification + password reset** — `email_verified_at` is in the M3
   schema; the actual verification email and reset-token flow need an email
   sender (transactional provider) that v1 does not otherwise need. M3+ or
   later.
3. **"Sign out everywhere"** — a `DELETE /api/auth/sessions` that clears all of
   a user's session rows. Trivial once sessions exist; not a v1 surface.
4. **Shared rate-limit store** — the in-process token bucket is single-instance
   only; a Redis/Postgres-backed limiter is needed before a multi-instance
   deploy.

### Gaps
5. **Logout is CSRF-open** — deliberate (forced logout has no data impact); a
   CSRF token on state-changing auth endpoints is a hardening item, not a v1
   blocker.
6. **M1 has no real accounts at all** — every deck is owned by the single
   `dev-local-user`. Multi-user ownership is exercised in tests by inserting a
   second user row directly; the real flow arrives at M3.

## References

- Code (M3): `backend/internal/middleware/session.go`,
  `backend/internal/middleware/devauth.go`, `backend/internal/authctx/context.go`,
  `backend/internal/sessioncookie/cookie.go`, `backend/internal/api/auth.go`,
  `backend/internal/db/queries/auth.sql`
- Code (M1): `devauth.go`, `authctx/context.go` only.
- Frontend: `frontend/src/lib/api.ts` (401 redirect), `frontend/next.config.ts`
  (same-origin proxy), `frontend/src/proxy.ts` (pass-through).
- Cross-segment: `deck-building` consumes `user_id` / `anon_token`;
  `platform-shell` selects the middleware in `server.New`.
