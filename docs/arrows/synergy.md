# Arrow: synergy

Per-commander card synergy computed from public Archidekt decklists and
gathered on demand: the `ensure` trigger, the `synergy_jobs` queue, one
background worker over a single paced Archidekt client, sampling and tally,
aggregates, the status/query endpoint, the builder's silent ensure call, and
the `cmd/synergysync` manual CLI.

## Status

**MAPPED** — LLD + specs authored 2026-10-19; reworked 2026-10-20 from a
configured-list batch job to on-demand gathering (captain's scope change,
`MANAFOLD.md` D7). SYN-001..042 and SYN-044 landed. Deferred: the attribution
requirement on a displaying surface (SYN-043, the wizard), bracket-sliced
samples (SYN-050), and partner pairs (SYN-051).

## References

### HLD
- docs/high-level-design.md (Approach — commander-specific synergy; Key Design Decisions — synergy source)
- MANAFOLD.md D7 (Archidekt option 1, on-demand gathering, and the accepted Terms-of-Use risk)

### LLD
- docs/intent/synergy/synergy-design.md

### EARS
- docs/intent/synergy/synergy-specs.md (SYN-001..005, SYN-010..013, SYN-020..022, SYN-030..037, SYN-040..042, SYN-044; SYN-043, SYN-050, SYN-051 deferred)

### Tests
- backend/internal/synergy/archidekt_test.go — SYN-002 (public search query, no auth), SYN-003 (identifying User-Agent, `Accept`, 6 s spacing), SYN-004 (2/4/8 min back-off, same request retried, fourth 429 fails), SYN-005 (403/404/410 skipped, other errors and undecodable bodies fail), SYN-034 (client job cap, pacing carried across jobs). Runs against `httptest` with a fake clock — no Archidekt call, no real waits.
- backend/internal/synergy/sample_test.go — SYN-002 (only search-returned candidates are fetched), SYN-005, SYN-010 (candidate filter, deck count, page cap, a repeated deck id fetched once), SYN-012, SYN-013 over recorded fixtures.
- backend/internal/synergy/tally_test.go — SYN-012 (maybeboard, commander, deleted, basics, quantity), SYN-013 (dedup, empty deck).
- backend/internal/synergy/options_test.go — SYN-001 (off by default, env limits and defaults).
- backend/internal/synergy/service_test.go (needs Postgres) — SYN-001 (disabled queues nothing, worker idle), SYN-030 / SYN-031 (ensure queues once, never calls Archidekt; one active job per commander in the schema), SYN-032 (daily cap), SYN-033 (worker lock), SYN-034 (baseline then commander, fresh baseline not resampled, job request cap), SYN-035 (job rows, orphaned running jobs requeued), SYN-036 (failure cooldown), SYN-037 (name resolution), SYN-011 (baseline sources), SYN-020 (no deck or user columns), SYN-021 / SYN-022 (synergy NULL until a baseline exists, recomputed when it lands; a failed job leaves previous aggregates), SYN-040 / SYN-041 (status, mirror-only commander-legal cards, banlist override, ordering, limit, stale sample served while refreshing).
- backend/internal/api/synergy_test.go (needs Postgres) — SYN-030 (ensure endpoint queues once, status follows), SYN-001 (disabled), SYN-040 (response shape, attribution, none-with-empty-cards), SYN-041, SYN-042 (404 unknown card, 400 bad id; 503 unconfigured).
- frontend/src/lib/synergy.test.ts — SYN-044 (ensure once per commander, failure swallowed).
- Fixtures: backend/internal/synergy/testdata/ (Archidekt-shaped search and deck responses; no owner data).

### Code
- backend/internal/synergy/ (`Service` — `Ensure`, `Status`, `Commander`, `Enqueue`; `RunWorker`, `Drain`, `runJob`, `planJob`; `sampleTarget`, `writeTarget`; `archidektClient`; `tallyDeck`, `sample`; `OptionsFromEnv`)
- backend/internal/api/synergy.go (`RegisterSynergyRoutes`, `ensureCommanderSynergy`, `getCommanderSynergy`)
- backend/cmd/api/main.go (starts `RunWorker`), backend/cmd/synergysync/main.go (manual CLI)
- backend/internal/db/migrations/000009_create_synergy.up.sql / .down.sql
- backend/internal/db/queries/synergy.sql
- frontend/src/lib/synergy.ts (`ensureOnce`), `api.ensureCommanderSynergy` in frontend/src/lib/api.ts, the effect in the builder page

## Architecture

A user action only queues. `ensure` writes a `synergy_jobs` row and returns the
commander's status; the one worker — holding a Postgres advisory lock, so it is
alone across processes — claims jobs oldest first, samples the identity baseline
when stale and then the commander through a single 6 s-paced client, and
replaces each target's aggregates in one transaction. The API and the future
wizard read the local tables through `Service.Commander`. Losing Archidekt
leaves the last aggregates in place and touches nothing else; `SYNERGY_ENABLED`
off stops all gathering.
