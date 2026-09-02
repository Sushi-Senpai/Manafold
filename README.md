# Manafold

AI-assisted Magic: The Gathering *Commander* deckbuilder — a Commander-first
builder on a local mirror of Scryfall data, with real-time legality validation
as a structural feature, deterministic deck analysis, and an AI layer that
curates and explains on top of decklist statistics.

See [`MANAFOLD.md`](./MANAFOLD.md) for the project vision and decision log, and
[`docs/high-level-design.md`](./docs/high-level-design.md) for the architecture.
Built with Linked-Intent Design from inception — read [`AGENTS.md`](./AGENTS.md)
before making changes.

## Layout

- `frontend/` — Next.js (App Router) + TypeScript + Tailwind
- `backend/` — Go API (chi, pgx, sqlc, golang-migrate)
- `docs/` — the LID arrow: HLD, per-segment LLDs + EARS specs, arrow overlay

## Local development

```bash
# Postgres
docker compose up -d

# Backend (from backend/, after copying .env.example to .env)
DEV_AUTH=true go run ./cmd/api   # applies pending migrations itself at startup

# Seed the card mirror from Scryfall bulk data (from backend/)
go run ./cmd/cardsync

# Frontend (from frontend/, after copying .env.example to .env.local)
yarn dev
```

`migrate` and `sqlc` are separate CLI tools (not Go dependencies):

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

After changing anything in `backend/internal/db/queries/` or `migrations/`, run
`sqlc generate` from `backend/` to regenerate `internal/db/generated/`.

## Card data & attribution

All card data is served from Manafold's own Postgres, mirrored daily from the
[Scryfall](https://scryfall.com) bulk-data exports (Oracle Cards + Default
Cards). Manafold is unofficial Fan Content permitted under the Fan Content
Policy. Not approved/endorsed by Wizards of the Coast. Portions of the materials
used are property of Wizards of the Coast. ©Wizards of the Coast LLC. Card data
and images © Wizards of the Coast, provided by Scryfall.
