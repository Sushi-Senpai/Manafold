# Arrow: synergy

Per-commander card synergy computed from public Archidekt decklists: the
`cmd/synergysync` batch job, the `internal/synergy` package (paced Archidekt
client, sampling, tally, aggregates, `TopCards`), `GET
/api/synergy/commanders/{cardId}`.

## Status

**MAPPED** — LLD + specs authored 2026-10-19 with the first slice; SYN-001..042
landed together. Deferred: bracket-sliced samples (SYN-050), partner pairs
(SYN-051), and a deployed cron (an operations step).

## References

### HLD
- docs/high-level-design.md (Approach — commander-specific synergy; Key Design Decisions — synergy source)
- MANAFOLD.md D7 (Archidekt option 1 and its accepted Terms-of-Use risk)

### LLD
- docs/intent/synergy/synergy-design.md

### EARS
- docs/intent/synergy/synergy-specs.md (SYN-001..005, SYN-010..013, SYN-020..022, SYN-030..032, SYN-040..043; SYN-050/051 deferred)

### Tests
- backend/internal/synergy/archidekt_test.go — SYN-002 (public search query, no auth), SYN-003 (identifying User-Agent, `Accept`, 6 s spacing), SYN-004 (2/4/8 min back-off, same request retried, fourth 429 fails), SYN-005 (403/404/410 skipped, other errors and undecodable bodies fail), SYN-030 (client budget). Runs against `httptest` with a fake clock — no Archidekt call, no real waits.
- backend/internal/synergy/sample_test.go — SYN-002 (only search-returned candidates are fetched), SYN-005, SYN-010 (candidate filter, deck count, page cap), SYN-012, SYN-013 over recorded fixtures.
- backend/internal/synergy/tally_test.go — SYN-012 (maybeboard, commander, deleted, basics, quantity), SYN-013 (dedup, empty deck).
- backend/internal/synergy/commanders_test.go — SYN-001 (list parsing, embedded default, file override).
- backend/internal/synergy/run_test.go (needs Postgres) — SYN-001 (unresolved names skipped), SYN-011 (baseline from the mirror), SYN-020 (no deck or user columns), SYN-021 (synergy = inclusion − baseline; NULL until a baseline exists), SYN-022 (failed run leaves previous aggregates), SYN-030 (a target starts only when its worst case fits), SYN-031 (a second run re-fetches nothing), SYN-032 (`synergy_runs` rows), SYN-040..042 via `TopCards` (mirror-only, commander-legal, banlist override, ordering, limit).
- backend/internal/api/synergy_test.go (needs Postgres) — SYN-040 (response shape, attribution, limit clamp), SYN-041 (banned and unknown cards never surface), SYN-042 (404 with no sample, 400 for a bad id).
- Fixtures: backend/internal/synergy/testdata/ (Archidekt-shaped search and deck responses; no owner data).

### Code
- backend/cmd/synergysync/main.go (env → `synergy.Options`, `synergy.Run`, non-zero exit on failure)
- backend/internal/synergy/ (`Run`, `plan`, `sampleTarget`, `writeTarget`; `archidektClient`; `tallyDeck`, `sample`; `LoadCommanders` + `commanders.txt`; `TopCards`)
- backend/internal/api/synergy.go (`RegisterSynergyRoutes`, `getCommanderSynergy`)
- backend/internal/db/migrations/000006_create_synergy.up.sql / .down.sql
- backend/internal/db/queries/synergy.sql

## Architecture

Archidekt is read only from the batch job, never on a request path. A run plans
the stale targets (one sample per configured commander plus one colour-identity
baseline per distinct identity), samples each within a request budget, and
replaces a target's aggregates in one transaction, recomputing synergy for its
identity. The API and the future wizard read the local tables through
`TopCards`. Losing Archidekt leaves the last aggregates in place and touches
nothing else.
