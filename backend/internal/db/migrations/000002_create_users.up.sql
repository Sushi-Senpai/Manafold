-- Minimal user identity for M1. The DevAuth stub upserts one fixed row here;
-- deck ownership hangs off it. The real email + password login flow (M3) adds
-- password_hash / email_verified_at in a later migration (see
-- docs/intent/account-access/). password_hash is nullable from the start so a
-- future OAuth-only identity needs no backfill.
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    password_hash TEXT,
    email_verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Server-side session rows (M3). Created, read, and eventually deleted or
-- expired, never edited — no updated_at. Present from M1 so SessionAuth and
-- its query compile; unused while DEV_AUTH=true.
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_user_id ON sessions (user_id);
