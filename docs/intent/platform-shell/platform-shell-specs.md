# Platform Shell — EARS Specs

## Backend Shell

- [x] **PLATFORM-001**: Before opening its database connection pool or serving any request, the system shall apply every pending embedded database migration to the configured database, and if a migration fails the system shall exit with a non-zero status and a descriptive message.
- [x] **PLATFORM-002**: When a client calls `GET /health`, the system shall respond `200` with a JSON body while the database is reachable, and `503` with a JSON body while it is not.
- [x] **PLATFORM-003**: If a required configuration variable (`DATABASE_URL` or `FRONTEND_URL`) is unset at startup, then the system shall fail to start with a message naming the missing variable.
- [x] **PLATFORM-004**: The system shall run no CORS middleware; every browser-facing `/api/*` request shall reach the backend only through the frontend's same-origin proxy rewrite (see `account-access`'s ACCT-016), never via a direct cross-origin call to the backend's own domain.
- [x] **PLATFORM-005**: The system shall register `/api` routes through per-domain registration helpers (`registerCardRoutes`, `registerDeckRoutes`, and their siblings), each defined beside the handlers it registers, rather than as one flat route list.
- [x] **PLATFORM-006**: The system shall build the `cmd/cardsync` standalone binary into the same container image as `cmd/api`, so the daily card sync runs as a separate process with no HTTP surface.
- [ ] **PLATFORM-007**: When the process receives a shutdown signal, the system shall stop accepting new connections and let in-flight requests finish before exiting.

## Frontend Substrate

- [x] **PLATFORM-020**: The system shall route every browser-facing request to `/api/*` through a same-origin rewrite on the frontend's own domain to the backend, so the browser never communicates with the backend's domain directly and the session cookie is scoped first-party to the frontend's origin.
- [x] **PLATFORM-021**: The system shall expose one shared frontend API client (`lib/api.ts`) through which every page makes its backend calls, providing a typed error type carrying the HTTP status and a JSON `Content-Type` default.
- [x] **PLATFORM-022**: The frontend Proxy (`src/proxy.ts`) shall be a pass-through that does not gate `(app)/*` routes on session-cookie presence; route protection is the client-side 401 handler's responsibility (see `account-access`'s ACCT-015).
- [x] **PLATFORM-023**: When the `.workspace` class wraps a subtree, the system shall render that subtree using the light workspace palette by overriding the semantic colour custom properties at runtime.
- [D] **PLATFORM-024**: The system shall render the brand mark as finished, designer-produced vector artwork rather than a plain text wordmark.
