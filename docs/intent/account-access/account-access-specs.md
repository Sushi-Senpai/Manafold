# Account & Access — EARS Specs

## Identity Middleware

- [x] **ACCT-001**: Where `DEV_AUTH=true`, the system shall attach an authenticated user ID to every `/api/*` request via a fixed dev-user stub (`dev-local-user`), upserting that user on first request, instead of session-based authentication (local development and CI only, off by default).
- [x] **ACCT-002**: The system shall read the caller's identity in every handler through the `authctx` accessors (`authctx.UserID` for a session user, `authctx.AnonToken` for an anonymous draft), resolved together by one helper (`callerOwner()`), independent of which auth middleware set them.
- [x] **ACCT-015**: When any frontend API request receives a `401` response, the system shall redirect the client to `/`, so pages carry no per-page logged-out logic.
- [x] **ACCT-016**: The system shall route every browser-facing request to `/api/*` through a same-origin rewrite on the frontend's own domain to the backend, so the session cookie is scoped first-party to the frontend's origin rather than as a third-party cookie subject to browser blocking.
- [x] **ACCT-003**: If an `/api/*` request outside `/api/auth/*`, `/api/public/*`, and `/health` carries no valid, unexpired session and no non-empty `X-Anon-Token` header, then the system shall respond `401` and shall not invoke the downstream handler.

## Email + Password (M3)

- [x] **ACCT-010**: When a client registers with an email and a password of at least 10 characters, the system shall reject a duplicate email with `409`, reject a malformed email or a short password with `422`, otherwise hash the password with argon2id (parameters encoded in the stored hash), create the user, create a 30-day session, and set the session cookie.
- [x] **ACCT-011**: When a client logs in, the system shall compare the supplied password against the stored hash — performing an equivalent dummy comparison when no user matches the email, so response time does not reveal account existence — create a session and set the cookie on success, and on any failure respond `401` with a single generic message that does not distinguish an unknown email from a wrong password.
- [x] **ACCT-012**: When a client logs out, the system shall delete the session row named by the cookie and clear the session cookie, succeeding even when the cookie names no live session.
- [x] **ACCT-013**: The system shall set the session cookie `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, with an expiry matching the session row's `expires_at`, and shall clear it with the same attributes.
- [x] **ACCT-014**: When a client calls `GET /api/auth/session`, the system shall respond `200` with whether a valid session is present and, when it is, the account's email — never `401`.
- [x] **ACCT-017**: If more than a fixed number of `/api/auth/login` or `/api/auth/register` requests arrive from one client IP within the rate-limit window, then the system shall respond `429` until the bucket refills, without consulting the database, keying the limiter on the `X-Forwarded-For` entry `TRUSTED_PROXY_COUNT` hops from the right (default `1`, fail-safe) and falling back to the connection's remote address, so a caller cannot reset its bucket by prepending a forged entry.
- [x] **ACCT-018**: The system shall store `password_hash` as nullable on `users` and model sessions independently of the authentication method, so an OAuth provider can be added later without migrating existing accounts.
- [x] **ACCT-019**: The system shall trim and lowercase a submitted email before its uniqueness check and before storage, so addresses differing only in case or surrounding whitespace resolve to one account.

## Anonymous Drafts (M3, owned with `deck-building`)

- [x] **ACCT-020**: Where a request carries no valid session but carries a non-empty `X-Anon-Token` header, the system shall resolve the caller to that token so `deck-building` can own decks by it; where both a valid session and the header are present, the session wins.
- [x] **ACCT-021**: When a client calls `POST /api/auth/claim-drafts` with an anonymous-draft token, the system shall respond `401` if the caller has no valid session, otherwise reassign every deck owned by that token to the caller and clear the token, treating an absent or unrecognised token as a successful no-op.

## Deferred

- [D] **ACCT-030**: The system shall support authenticating via one or more OAuth providers through a `user_identities` table and the authorization-code flow.
- [D] **ACCT-031**: The system shall send a verification email on registration and support a password-reset token flow.
- [D] **ACCT-032**: When a client calls `DELETE /api/auth/sessions`, the system shall delete every session row for that user.
