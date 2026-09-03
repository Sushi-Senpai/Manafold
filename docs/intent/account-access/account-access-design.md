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

Every handler reads the caller's identity through one accessor —
`authctx.UserID(ctx)` for an authenticated user, `authctx.AnonToken(ctx)` for an
anonymous-draft caller — set by whichever auth middleware ran. The middleware
**fails closed** — with neither a valid session nor an accepted anonymous token
it responds `401` and does not call the downstream handler.

## Identity Middleware

`internal/middleware`:

- **`DevAuth(queries)`** — active only when `DEV_AUTH=true` (off by default, and
  never in a deployed environment — see `internal/config`). Upserts one fixed
  user (`dev@manafold.local`) and attaches its ID to every request. No cookie,
  no token. Lets a contributor and CI run every other segment with no account
  infrastructure.
- **`AnonOrSession(queries)`** — the default protected-group middleware. It
  reads the session cookie and calls `GetSession` (whose SQL filters
  `expires_at > now()`, so "no rows" covers missing *and* expired); on a hit it
  attaches `authctx.WithUserID(ctx, session.UserID)`. On a miss it falls back to
  a non-empty `X-Anon-Token` header and attaches `authctx.WithAnonToken`. With
  neither it responds `401`. A valid session always wins over a supplied token
  (ACCT-020).

`server.New` selects one: `if d.DevAuth { r.Use(DevAuth) } else {
r.Use(AnonOrSession) }`. `/api/auth/*`, `/health`, and `/public/*` are the only
routes outside the protected group. `/api/auth/claim-drafts` resolves the
session itself from the cookie (it needs the caller's real user ID even though
it sits in the unauthenticated group).

## Email + Password

### Schema (migration `000002_create_users`)

The `users` credential columns and the `sessions` table are present from the
first migration; `DevAuth` writes only `users`, the email + password flow uses
both.

- `users` — `id uuid pk`, `email text not null unique`, `name text`,
  `password_hash text null` (null for a future OAuth-only user),
  `email_verified_at timestamptz null`, `created_at` / `updated_at`. No
  `add_credentials` follow-up migration — the credential columns are present
  from the start.
- `sessions` — `id uuid pk`, `user_id uuid fk → users on delete cascade`,
  `expires_at timestamptz not null`, `created_at timestamptz not null`. Index on
  `user_id`. No `updated_at` (a session is created, read, deleted/expired, never
  edited).

### Password hashing

`internal/passwordhash` owns it. **argon2id** via `golang.org/x/crypto/argon2`
(`IDKey`), with `m = 19456` KiB (19 MiB), `t = 2`, `p = 1`, a 16-byte random
salt, and a 32-byte key — the OWASP second-choice profile, chosen deliberately
low on memory so concurrent logins stay within the Render free tier's container
ceiling. The parameters are written into the encoded hash string
(`$argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 hash>`), so they can be raised
later with no schema change: `Verify` reads whatever parameters a stored hash
carries. `Verify` also accepts a bcrypt hash (`$2a$` / `$2b$` / `$2y$` prefix)
via `golang.org/x/crypto/bcrypt`, so bcrypt stays a drop-in fallback and old
hashes keep verifying if the KDF is ever switched. `Verify` returns
`(false, nil)` for a wrong password and an error only for a malformed encoded
hash.

### Endpoints (`/api/auth/*`, unauthenticated)

| Endpoint | Method | Body | Behaviour |
|---|---|---|---|
| `/api/auth/register` | POST | `{ email, password }` | trim + lowercase the email (ACCT-019); `422` on a malformed email or a password under 10 chars; `409` if the email exists; hash; create the user; create a 30-day session; set the cookie; return `{ authenticated: true, email }` |
| `/api/auth/login` | POST | `{ email, password }` | trim + lowercase the email; verify against the stored hash, running a dummy verify when no user matches so timing does not leak account existence; on success create a session + set the cookie and return `{ authenticated: true, email }`; on any failure `401` with one generic message |
| `/api/auth/logout` | POST | — | delete the session row named by the cookie (a no-op if it names none), clear the cookie, `204`; CSRF-open by design (forced logout only, no data impact) |
| `/api/auth/session` | GET | — | `200 { authenticated: bool, email? }`; used by the landing page and the app header; never `401` |
| `/api/auth/claim-drafts` | POST | `{ anon_token }` | `401` unless the cookie names a live session; otherwise `ClaimAnonDecks` reassigns every deck owned by that token to the caller and nulls the token; an absent or unknown token is a `200` no-op; returns `{ claimed: <count> }` |

### Session cookie

`internal/sessioncookie` owns the name (`manafold_session`) and flags
(`HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, 30-day expiry matching the
session row). `SameSite=Lax` is sufficient because the same-origin proxy
(`ACCT-016`) makes it a first-party cookie of the frontend origin.

### Rate limiting

`/api/auth/login` and `/api/auth/register` are rate-limited per client IP by
`internal/ratelimit` — an in-process token bucket, capacity 10, refilling 1
token every 6 seconds (so a burst of 10 then ~10/min sustained). The check runs
before any database work; an empty bucket returns `429`. The client IP is the
left-most `X-Forwarded-For` entry (the browser, as seen past the Vercel proxy
and Render's load balancer) falling back to `RemoteAddr`. A shared store is
needed only if the backend scales past one instance (Open Question 4).

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

## Anonymous Deck Drafts

A first-time visitor can build a deck without signing in. `lib/api.ts` mints an
opaque token (`crypto.randomUUID()`, stored in `localStorage` as
`manafold_anon`) and sends it as an `X-Anon-Token` header on every API request.
`AnonOrSession` resolves an unauthenticated caller to that token; `deck-building`
scopes its queries to a polymorphic owner key — `user_id` or `anon_token`, one
non-null (the `decks` CHECK enforces exactly one). See
`docs/intent/deck-building/deck-building-design.md § Ownership` for the query
shape.

On sign-in and on register, the frontend calls `POST /api/auth/claim-drafts`
with the token; `ClaimAnonDecks` reassigns every `decks` row owned by that token
to the new `user_id` and nulls the token in one `UPDATE`. The call is
idempotent — a second claim of the same token matches no rows and returns
`{ claimed: 0 }`. The frontend keeps the token in `localStorage` afterwards (it
now owns nothing; a later anonymous deck reuses it).

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Auth mechanism (v1) | Manafold email + password (argon2id) + server-side sessions | Google OAuth (the research recommendation); passwordless magic links; a hosted identity provider | The captain's explicit call for the first pass. The `password_hash`-nullable `users` row and the provider-agnostic session model keep OAuth addable later without a migration of existing accounts. |
| Password KDF | argon2id, parameters in the encoded hash string; bcrypt as a documented fallback | bcrypt only; scrypt; PBKDF2 | argon2id is the current best-practice memory-hard KDF; encoding the parameters lets them rise later with no schema change. bcrypt stays a fallback because Render's free tier caps container memory and aggressive argon2 `m` can be tight. |
| Session vs JWT | Server-side session rows | Stateless signed JWTs | A session row is revocable immediately (logout, "sign out everywhere", a compromised account); a JWT is valid until expiry. |
| Login failure response | One generic `401` for both unknown-email and wrong-password | Distinct messages | Distinct messages are a user-enumeration oracle. |
| Anonymous drafts | `decks.user_id` nullable + `anon_token` + a claim endpoint; CHECK enforces exactly one owner | Require sign-in before creating a deck; store anon decks only in `localStorage` | Lowering the barrier matters for a hobby tool; a server-side anon row means the draft survives a browser change once claimed and is the same `decks` row throughout. |
| Dev auth | Keep a `DEV_AUTH=true` fixed-user stub, off by default | Only ever run real auth locally | Contributors and CI exercise every other segment without standing up a session store; the deck integration tests inject identity through `authctx` directly and never need it, but it keeps the app runnable end to end without email + password. |
| One middleware for session + anon | `AnonOrSession` tries the session cookie, then the `X-Anon-Token` header | Two middlewares chained; a separate `/api/anon/*` route tree | The two paths differ only in which `authctx` setter they call and share the "else 401" tail; one middleware keeps the protected group's wiring a single `r.Use`. |
| Client IP source | Left-most `X-Forwarded-For`, then `RemoteAddr` | `RemoteAddr` only; a signed forwarded header | Behind Vercel's rewrite and Render's load balancer `RemoteAddr` is always an infrastructure hop; the left-most XFF entry is the closest available approximation of the real client for a rate-limit key. Not security-critical — worst case a shared NAT shares a bucket. |

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
   blocker. Register and login are not CSRF-sensitive (they establish, not
   mutate, a session).
6. **No email verification gate** — `email_verified_at` is stored but never
   checked; an unverified account can log in and build decks. Gating any
   surface on verification waits on the transactional email sender (Deferred 2).
7. **`X-Anon-Token` is unauthenticated by nature** — anyone presenting a token
   string owns its drafts. That is the intended trade for a zero-friction first
   deck; the blast radius is one visitor's unclaimed drafts, and claiming binds
   them to a real account.

## References

- Code: `backend/internal/middleware/session.go` (`AnonOrSession`),
  `backend/internal/middleware/devauth.go`, `backend/internal/authctx/context.go`,
  `backend/internal/sessioncookie/cookie.go`, `backend/internal/api/auth.go`,
  `backend/internal/passwordhash/passwordhash.go`,
  `backend/internal/ratelimit/ratelimit.go`,
  `backend/internal/db/queries/{users,sessions}.sql`,
  `backend/internal/db/queries/decks.sql` (`ClaimAnonDecks`).
- Frontend: `frontend/src/lib/api.ts` (401 redirect, anon token, auth calls),
  `frontend/src/lib/auth.ts`, `frontend/src/app/(auth)/login`,
  `frontend/src/app/(auth)/register`, `frontend/next.config.ts` (same-origin
  proxy), `frontend/src/proxy.ts` (pass-through).
- Cross-segment: `deck-building` scopes ownership by `user_id` / `anon_token`
  and owns `ClaimAnonDecks` + DECK-040/041; `platform-shell` selects the
  middleware in `server.New`.
