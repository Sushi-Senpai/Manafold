# Synergy Data — EARS Specs

## Batch Job — Source & Etiquette

- [x] **SYN-001**: When the synergy job runs, the system shall read the commander list — one name per line, blank lines and `#` comments ignored — from the file named by `SYNERGY_COMMANDERS_FILE`, or from the embedded default list when that variable is unset, resolve each name against `cards` (case-insensitive exact name or the front face of a multi-face name, `can_be_commander` true), and log and skip any name that does not resolve rather than failing the run.
- [x] **SYN-002**: The synergy job shall read Archidekt only through the public deck search (`/api/decks/v3/?deckFormat=3&commanderName=…&orderBy=-createdAt&page=…`) and public deck detail (`/api/decks/{id}/`) endpoints, without authentication, and shall never enumerate deck ids or request a deck its search did not return.
- [x] **SYN-003**: Every Archidekt request the synergy job makes shall carry the User-Agent `Manafold-synergy/1.0 (+https://github.com/Sushi-Senpai/Manafold)` (identifying the project, containing no personal data) and `Accept: application/json`, and shall start no less than 6 seconds after the previous request started.
- [x] **SYN-004**: When Archidekt answers a request with HTTP `429`, the synergy job shall wait an escalating back-off — 2, then 4, then 8 minutes — before retrying the same request, without relying on a `Retry-After` header, and if a fourth consecutive `429` arrives the job shall stop the run as failed.
- [x] **SYN-005**: When a deck detail request answers `403`, `404`, or `410`, the synergy job shall skip that deck and continue the sample; any other non-2xx answer or undecodable body shall stop the run as failed.

## Batch Job — Sampling & Tallying

- [x] **SYN-010**: When sampling a commander, the synergy job shall fetch, newest first, up to the configured deck count (default 60) of the decks returned by that commander's search across at most the configured number of search pages (default 3), considering only results that are public (`private` and `unlisted` false), Commander format (`deckFormat` 3), and at least 98 cards in size.
- [x] **SYN-011**: When sampling the baseline for a colour identity, the synergy job shall take the configured number (default 5) of highest-`edhrec_rank` cards in the mirror that can be a commander, are commander-legal, have a commander colour identity exactly equal to that identity, and are not on the commander list, and shall sample the configured number (default 12) of recent decks of each under the SYN-010 candidate rules.
- [x] **SYN-012**: When tallying a deck, the system shall count each distinct oracle id once regardless of quantity, excluding card entries with a non-null `deletedAt`, entries in any category whose deck-level `includedInDeck` is false (such as the maybeboard), entries in the `Commander` category, the sampled commander's own oracle id, and basic lands.
- [x] **SYN-013**: When a deck's tallied card set is identical to that of a deck already counted in the same sample, the system shall not count it and shall add it to the sample's duplicates-skipped count; a deck whose tallied set is empty shall not be counted.

## Storage & Synergy

- [x] **SYN-020**: The system shall store, per sampled commander, its commander oracle id, name, canonical colour identity, counted deck total, duplicates-skipped count, and fetch time, and per card the number of sampled decks containing it and its inclusion rate; and per colour-identity baseline the same totals plus the baseline commanders' names; and shall store no decklist, deck id, deck name, owner, or other user data.
- [x] **SYN-021**: The system shall compute each commander card's synergy as its inclusion rate minus the baseline inclusion rate for the same card in the commander's colour identity — treating a card absent from a non-empty baseline as rate 0 — and shall store a null baseline rate and null synergy while that identity has no baseline sample.
- [x] **SYN-022**: When a commander or baseline sample completes, the system shall replace that target's previous aggregates and recompute synergy for every commander in the affected colour identity in a single transaction, so that a failure before or during the write leaves the previous aggregates intact.

## Run Control

- [x] **SYN-030**: The synergy job shall make no more than `SYNERGY_MAX_REQUESTS` (default 600) Archidekt requests per run, and shall start a target only when the remaining budget covers that target's worst-case request count, leaving any remaining targets pending for the next run.
- [x] **SYN-031**: When a run starts, the synergy job shall refresh only targets never sampled or whose last sample is older than `SYNERGY_REFRESH_DAYS` (default 7), stalest first, so that a run that stopped for any reason is continued by the next run without re-fetching targets it completed.
- [x] **SYN-032**: The synergy job shall record a `synergy_runs` row per run carrying its status, requests made, targets refreshed, targets left pending, start and finish times, and error text on failure, and the process shall exit non-zero when the run fails.

## Query & Display

- [x] **SYN-040**: When a client calls `GET /api/synergy/commanders/{cardId}`, the system shall return, from the local database only, that commander's top synergy cards — ordered by synergy descending with nulls last, then inclusion descending, then name, capped at `limit` (default 30, maximum 100) — each with its mirror card summary, deck count, inclusion rate, baseline rate, and synergy, plus the sample's deck count, duplicates skipped, fetch time, baseline deck count, and baseline fetch time, and the attribution text "Synergy data from public Archidekt decks" with the link `https://archidekt.com`.
- [x] **SYN-041**: The synergy query shall return only cards that resolve to a `cards` row that is commander-legal and not banned by `banlist_overrides`.
- [x] **SYN-042**: If `GET /api/synergy/commanders/{cardId}` names a commander with no stored sample, then the system shall respond `404`.
- [x] **SYN-043**: While a deck has a commander assigned, the builder shall show a synergy panel listing that commander's top synergy cards with synergy and inclusion percentages and an add-to-main action — showing "In deck" instead for cards already on the main or command board — and a footer giving the sample's deck count, fetch date, and the attribution "Synergy data from public Archidekt decks" linked to Archidekt; when the commander has no data the panel shall say so.

## Deferred

- [D] **SYN-050**: The system shall slice synergy samples by Archidekt's declared `edhBracket` once enough decks declare one for per-bracket samples to be meaningful.
- [D] **SYN-051**: The system shall sample partner-commander pairs as one commander.
