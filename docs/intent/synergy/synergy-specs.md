# Synergy Data — EARS Specs

Markers: `[x]` implemented, `[ ]` active gap, `[D]` deferred.

## Source & Etiquette

- [x] **SYN-001**: Unless `SYNERGY_ENABLED` is `true`, the system shall queue no gathering job, start no worker, and make no Archidekt request, and shall report every commander's status as `none` with reason `disabled`.
- [x] **SYN-002**: The synergy worker shall read Archidekt only through the public deck search (`/api/decks/v3/?deckFormat=3&commanderName=…&orderBy=-createdAt&page=…`) and public deck detail (`/api/decks/{id}/`) endpoints, without authentication, and shall never enumerate deck ids or request a deck its search did not return.
- [x] **SYN-003**: Every Archidekt request the synergy worker makes shall carry the User-Agent `Manafold-synergy/1.0 (+https://github.com/Sushi-Senpai/Manafold)` (identifying the project, containing no personal data) and `Accept: application/json`, and shall start no less than 6 seconds after the previous request started, whichever job or commander that request belongs to.
- [x] **SYN-004**: When Archidekt answers a request with HTTP `429`, the synergy worker shall wait an escalating back-off — 2, then 4, then 8 minutes — before retrying the same request, without relying on a `Retry-After` header, and if a fourth consecutive `429` arrives the system shall fail the job.
- [x] **SYN-005**: When a deck detail request answers `403`, `404`, or `410`, the synergy worker shall skip that deck and continue the sample; any other non-2xx answer or undecodable body shall fail the job.

## Sampling & Tallying

- [x] **SYN-010**: When sampling a commander, the synergy worker shall fetch, newest first, up to `SYNERGY_DECKS_PER_COMMANDER` (default 40) of the decks returned by that commander's search across at most `SYNERGY_SEARCH_PAGES` (default 3) search pages, considering only results that are public (`private` and `unlisted` false), Commander format (`deckFormat` 3), and at least 98 cards in size, and shall fetch a deck id that the search returns more than once only once.
- [x] **SYN-011**: When sampling the baseline for a colour identity, the synergy worker shall take the `SYNERGY_BASELINE_COMMANDERS` (default 5) highest-`edhrec_rank` cards in the mirror that can be a commander, are commander-legal, have a commander colour identity exactly equal to that identity, are not the commander being gathered, and have no stored synergy sample, and shall sample `SYNERGY_DECKS_PER_BASELINE_COMMANDER` (default 8) recent decks of each under the SYN-010 candidate rules.
- [x] **SYN-012**: When tallying a deck, the system shall count each distinct oracle id once regardless of quantity, excluding card entries with a non-null `deletedAt`, entries in any category whose deck-level `includedInDeck` is false (such as the maybeboard), entries in the `Commander` category, the sampled commander's own oracle id, and basic lands.
- [x] **SYN-013**: When a deck's tallied card set is identical to that of a deck already counted in the same sample, the system shall not count it and shall add it to the sample's duplicates-skipped count; a deck whose tallied set is empty shall not be counted.

## Storage & Synergy

- [x] **SYN-020**: The system shall store, per sampled commander, its commander oracle id, name, canonical colour identity, counted deck total, duplicates-skipped count, and fetch time, and per card the number of sampled decks containing it and its inclusion rate; and per colour-identity baseline the same totals plus the baseline commanders' names; and shall store no decklist, deck id, deck name, owner, or other user data.
- [x] **SYN-021**: The system shall compute each commander card's synergy as its inclusion rate minus the baseline inclusion rate for the same card in the commander's colour identity — treating a card absent from a non-empty baseline as rate 0 — and shall store a null baseline rate and null synergy while that identity has no baseline sample.
- [x] **SYN-022**: When a commander or baseline sample completes, the system shall replace that target's previous aggregates and recompute synergy for every commander in the affected colour identity in a single transaction, so that a failure before or during the write leaves the previous aggregates intact.

## On-Demand Gathering

- [x] **SYN-030**: When a client calls `POST /api/synergy/commanders/{cardId}/ensure`, the system shall answer from the local database only, without waiting on Archidekt, with the commander's synergy status; and where `SYNERGY_ENABLED` is `true`, the card can be a commander and is commander-legal, its stored sample is not fresher than `SYNERGY_TTL_DAYS` (default 14), it has no queued or running job, and the daily cap allows it, the system shall also queue one gathering job for it.
- [x] **SYN-031**: The system shall hold at most one queued or running job per commander, enforced by the database, and shall not queue a job for a commander whose stored sample is fresher than `SYNERGY_TTL_DAYS`.
- [x] **SYN-032**: The system shall queue no more than `SYNERGY_MAX_NEW_COMMANDERS_PER_DAY` (default 10) jobs in any rolling 24 hours, and when that cap blocks a commander it shall report that commander's status as `none` with reason `daily_cap`.
- [x] **SYN-033**: One synergy worker shall drain the job queue for the whole deployment, oldest job first and one job at a time, and shall run only while it holds a Postgres advisory lock, so that the SYN-003 pace holds across API instances and the CLI.
- [x] **SYN-034**: When the worker runs a job, it shall sample the commander's colour-identity baseline first if the identity has no baseline or its baseline is not fresher than `SYNERGY_TTL_DAYS`, then sample the commander, and shall fail the job if it would exceed the job's request cap — the targets' worst-case request count plus a fixed allowance for `429` retries — without writing a target whose sample did not complete.
- [x] **SYN-035**: The system shall record each job's status (`queued`, `running`, `succeeded`, `failed`), requests made, requested, started and finished times, and error text on failure, and when the worker starts it shall return jobs left `running` by a dead process to `queued`.
- [x] **SYN-036**: When a job fails, the worker shall wait `SYNERGY_FAILURE_COOLDOWN_MINUTES` (default 15) before starting another job.
- [x] **SYN-037**: When `cmd/synergysync` is run with a commander name, the system shall resolve the name against `cards` (case-insensitive exact name or the front face of a multi-face name, `can_be_commander` true), queue a job for it that the daily cap does not block, drain the queue through the same worker code, and exit non-zero if the name does not resolve, another process holds the worker lock, or a job fails.

## Query & Attribution

- [x] **SYN-040**: When a client calls `GET /api/synergy/commanders/{cardId}`, the system shall return, from the local database only, that commander's status — `ready` when its stored sample is fresher than `SYNERGY_TTL_DAYS`, otherwise `gathering` when a job is queued or running, otherwise `none` — and, when any sample is stored, that commander's top synergy cards — ordered by synergy descending with nulls last, then inclusion descending, then name, capped at `limit` (default 30, maximum 100) — each with its mirror card summary, deck count, inclusion rate, baseline rate, and synergy, plus the sample's deck count, duplicates skipped, fetch time, baseline deck count, and baseline fetch time, and the attribution text "Synergy data from public Archidekt decks" with the link `https://archidekt.com`.
- [x] **SYN-041**: The synergy query shall return only cards that resolve to a `cards` row that is commander-legal and not banned by `banlist_overrides`.
- [x] **SYN-042**: If `GET /api/synergy/commanders/{cardId}` or the ensure endpoint names a card id that is not in the mirror, then the system shall respond `404`; a commander with no stored sample shall instead return its status with an empty card list and a null sample.
- [x] **SYN-044**: When the builder loads a deck that has a commander assigned, it shall call the ensure endpoint for that commander once and shall show nothing from the answer.
- [ ] **SYN-043**: Where a surface displays synergy data, the surface shall show the text "Synergy data from public Archidekt decks" linked to `https://archidekt.com`, and the deck count of the sample the numbers come from. (First displayed by the deck wizard's picks step, `WIZ-065`.)

## Deferred

- [D] **SYN-050**: The system shall slice synergy samples by Archidekt's declared `edhBracket` once enough decks declare one for per-bracket samples to be meaningful.
- [D] **SYN-051**: The system shall sample partner-commander pairs as one commander.
