---
parent: high-level-design
prefix: SYN
---

# Synergy Data

## Context and Design Philosophy

Manafold's recommendation tenet is *the model curates and explains; statistics
and rules decide*. `edhrec_rank` says how popular a card is overall; it cannot
say which cards belong with a particular commander. This segment supplies that
commander-specific signal: **per-commander card inclusion rates computed from
public Archidekt decklists, compared against a colour-identity baseline**. The
deck-building wizard is the main consumer.

**Synergy** for a card *c* under commander *K* is

```
synergy(c, K) = inclusion(c among K's decks) − inclusion(c among baseline decks for K's colour identity)
```

where an inclusion rate is the fraction of sampled decks that contain the card.
A staple every deck plays (Sol Ring) scores near zero. A card mostly played with
*K* (Goblin Warchief under Krenko) scores high. Another commander's signature
payoff scores negative. This is the same shape EDHREC publishes; EDHREC has no
open API, which is why Manafold computes it itself.

**Gathered on demand.** Nothing is crawled on a schedule and there is no
commander list. When a user builds a deck for a commander that has no fresh
synergy data, the builder (and later the wizard) asks the backend to *ensure*
that commander. The ensure call only enqueues a job and returns the commander's
status; **a client request never waits on Archidekt**. One background worker
drains the queue. Readers see `gathering` until the data is `ready`, and the
wizard falls back to open-data picks (`edhrec_rank`, Scryfall oracle tags)
meanwhile.

Archidekt has no documented API, but its public read endpoints are open with no
auth. Archidekt's developer said in 2019 that the read API is open and public,
asked for a link back, and warned they may lock it down if traffic becomes a
burden. Their Terms of Use are stricter: personal non-commercial use, no
automated queries. Manafold is a personal, non-monetised tool for the captain
and friends, and the captain accepted that risk (see `MANAFOLD.md` decision D7).
Because user actions now trigger the gathering, the exposure is larger than a
fixed schedule would be, and every choice here bounds it: one global
request pace, a per-day cap on newly gathered commanders, a per-commander deck
cap, a freshness TTL that keeps a gathered commander from being refetched for
two weeks, an off-by-default switch, only aggregates stored, and attribution
wherever the data appears. The design assumes the source **can disappear
without notice**: everything here is additive, and a lockdown leaves the last
aggregates in place and the rest of the product untouched.

The segment depends on `card-data` (commanders resolve against `cards`, the
baseline commanders are chosen from the mirror, and every displayed card is a
mirror row) and on `platform-shell` (router, migrations, `cmd/` binaries).
`ai-assist` and the wizard read its query; nothing in `deck-building` depends on
it.

## Data Source: Archidekt

Only two public, unauthenticated endpoints are used. Deck-id enumeration and
any private-deck or authenticated access are out of scope.

| Purpose | Request |
|---|---|
| Recent public Commander decks for one commander | `GET https://archidekt.com/api/decks/v3/?deckFormat=3&commanderName=<name>&orderBy=-createdAt&page=<n>` |
| One deck's cards | `GET https://archidekt.com/api/decks/<id>/` |

- **Search** returns `{count, next, results[]}`, 60 results per page. `count`
  is capped at 1,000. Each result carries `id`, `size`, `deckFormat`,
  `private`, and `unlisted`. A page beyond the window returns no results.
- **Deck detail** carries deck-level `categories[]` (`name`, `includedInDeck`)
  and `cards[]`. Each card entry has `quantity`, `categories[]` (names),
  `deletedAt`, and `card.oracleCard` with `uid` (equal to the Scryfall
  `oracle_id`, i.e. `cards.scryfall_oracle_id`), `name`, and `superTypes`.
  Decks therefore join onto the mirror with no name resolution.
- **Commander name.** `commanderName` takes the commander's printed name. For a
  multi-face card the mirror's `A // B` name is sent as its front face `A`.
  Partner pairs are out of scope: each commander is sampled on its own.

### HTTP etiquette

- **User-Agent** `Manafold-synergy/1.0 (+https://github.com/Sushi-Senpai/Manafold)`
  plus `Accept: application/json`. The User-Agent identifies the project and
  contains no personal data.
- **One global pace.** The worker owns the only Archidekt client. Successive
  requests, across every job and every commander, start at least **6 s** apart
  (the investigation saw no `429` at that pace; 2 s tripped it within about ten
  requests).
- **429 back-off.** Archidekt sends no `Retry-After`. On a `429` the worker
  waits an escalating back-off (2 min, then 4 min, then 8 min) before retrying
  the same request. A fourth consecutive `429` fails the job.
- **Bounded request phases.** Each request runs under a per-request timeout (a
  deck detail is a few hundred KB), so a stalled connection cannot hang the
  worker.
- A deck that answers `403` / `404` / `410` between search and fetch (made
  private or deleted) is skipped. Any other non-2xx fails the job.

## Sampling

A **target** is one sample the worker takes.

- **Commander target.** Up to `SYNERGY_DECKS_PER_COMMANDER` (default 40) of the
  commander's most recent public decks, reading at most `SYNERGY_SEARCH_PAGES`
  (default 3) search pages to find them.
- **Baseline target**, one per colour identity, shared by every commander in
  that identity. It samples `SYNERGY_DECKS_PER_BASELINE_COMMANDER` (default 8)
  recent decks from each of the `SYNERGY_BASELINE_COMMANDERS` (default 5)
  highest-ranked *other* commanders in that identity. Those are chosen from the
  mirror: `can_be_commander`, commander-legal, commander colour identity
  exactly equal to the target identity, not the commander being gathered, not a
  commander that already has a stored sample, ordered by `edhrec_rank` (nulls
  last), then name. An identity with no such commander in the mirror gets an
  empty baseline.

A search result is a **candidate** only if it is public (`private` and
`unlisted` false), Commander (`deckFormat = 3`), and has `size ≥ 98`, which
filters out unfinished stubs. Candidates are fetched newest first until the
sample is full or the pages run out.

### Tallying a deck

Each fetched deck becomes a **set** of oracle ids:

- **Excluded:**
  - card entries with a non-null `deletedAt`;
  - entries in any category whose deck-level `includedInDeck` is `false`
    (Maybeboard, Sideboard, and any other out-of-deck category);
  - entries in the `Commander` category;
  - the target's own commander oracle id (even when the deck does not tag it);
  - basic lands (`superTypes` contains `Basic`).
- **Quantity is ignored:** the question is "does this deck play the card".

**Deduplication.** Copies and clones are common, and two identical decklists
are one data point. Within one target's sample, a deck whose tallied set is
identical to an earlier deck's (compared by a SHA-256 of the sorted oracle ids)
is not counted. The duplicate count is recorded. A deck with an empty tallied
set is skipped as unusable. A deck id that a target's search pages return twice
(a new deck shifting the newest-first order between page requests) is fetched
once.

## Storage

Migration `create_synergy`. Only aggregates and job bookkeeping are stored.
Decklists, deck ids, deck names, owners, and any other user data are never
persisted; a sample lives in memory only while its target is being gathered.

### `synergy_commanders`

| column | type | notes |
|---|---|---|
| `commander_oracle_id` | uuid pk, fk → `cards(scryfall_oracle_id)` on delete cascade | |
| `commander_name` | text not null | the commander's mirror name |
| `color_identity` | text not null | canonical WUBRG-ordered letters; `C` for colourless |
| `deck_count` | int not null | decks counted (after dedup) |
| `duplicates_skipped` | int not null | |
| `fetched_at` | timestamptz not null | when the sample was taken; the TTL clock |

### `synergy_commander_cards`

| column | type | notes |
|---|---|---|
| `commander_oracle_id` | uuid fk → `synergy_commanders` on delete cascade | pk part |
| `card_oracle_id` | uuid | pk part. No FK: a brand-new card may predate the next Scryfall sync. The join happens at read time. |
| `deck_count` | int not null | sampled decks containing the card |
| `inclusion_rate` | double precision not null | `deck_count / synergy_commanders.deck_count` |
| `baseline_rate` | double precision null | the identity baseline's rate for this card; `0` when absent from a non-empty baseline; `NULL` when the identity has no baseline sample |
| `synergy` | double precision null | `inclusion_rate − baseline_rate`; `NULL` with `baseline_rate` |

Index: `(commander_oracle_id, synergy DESC NULLS LAST)`.

### `synergy_baselines` / `synergy_baseline_cards`

These mirror the commander pair, keyed by `color_identity`.
`synergy_baselines` also records `baseline_commanders text[]` (whose decks
formed the sample) and `fetched_at`. `synergy_baseline_cards` holds
`deck_count` and `inclusion_rate` per card.

### `synergy_jobs`

The queue and its audit trail in one table.

| column | type | notes |
|---|---|---|
| `id` | uuid pk | |
| `commander_oracle_id` | uuid not null, fk → `cards(scryfall_oracle_id)` on delete cascade | |
| `status` | text not null | `queued` / `running` / `succeeded` / `failed` |
| `requested_at` | timestamptz not null | the daily cap counts rows by this |
| `started_at`, `finished_at` | timestamptz null | |
| `requests_made` | int not null | Archidekt requests the job made, retries included |
| `error` | text null | failure text |

A partial unique index on `commander_oracle_id` where `status` is `queued` or
`running` makes "at most one active job per commander" a database guarantee,
not just an application check.

### Write path

A target's aggregates are written in **one transaction, only after its whole
sample has been fetched**:

1. Upsert the header row.
2. Delete the target's old card rows and `COPY` the new ones.
3. Recompute `baseline_rate` / `synergy` for every commander in the affected
   colour identity.

A job that dies mid-target leaves that target's previous aggregates intact.
Recomputing on both sides (after a commander write and after a baseline write)
means synergy is never left computed against a stale baseline, whatever order
the targets are gathered in.

## On-Demand Gathering (`internal/synergy`)

`synergy.Service` is the entry point: the API calls `Ensure` and `Status`, the
API process runs `RunWorker`, and `cmd/synergysync` calls `Enqueue` and
`Drain`.

### Ensure (the trigger)

`Ensure(commanderCardID)` runs on `POST /api/synergy/commanders/{cardId}/ensure`.
It reads and writes only Manafold's database:

1. The card must be a commander (`can_be_commander`, commander-legal);
   otherwise the status is `none`.
2. If the commander's stored sample is fresher than `SYNERGY_TTL_DAYS` (default
   14), the status is `ready` and nothing is queued.
3. If the commander already has a queued or running job, the status is
   `gathering` and nothing is queued.
4. If `SYNERGY_MAX_NEW_COMMANDERS_PER_DAY` (default 10) jobs have already been
   requested in the last 24 hours, nothing is queued and the status is `none`
   with reason `daily_cap`.
5. Otherwise one job is inserted `queued` and the status is `gathering`.

When `SYNERGY_ENABLED` is not `true`, step 1 answers `none` with reason
`disabled` for everything: nothing is queued and the worker never starts. The
switch is off by default, like `AI_ENABLED`, so a fresh checkout makes no
Archidekt request.

The builder fires `ensure` once when it loads a deck with a commander assigned
and shows nothing from it; the wizard will call the same endpoint.

### Status

| status | meaning |
|---|---|
| `none` | no fresh sample and nothing queued (never requested, daily cap, disabled, or the last job failed) |
| `gathering` | a job is queued or running |
| `ready` | a sample fresher than the TTL is stored |

A stale sample is still readable: the query returns its cards whenever any
sample is stored, with its fetch time, and the status says whether it is being
refreshed. A caller that needs fresh data waits for `ready`.

### The worker

One worker drains the queue for the whole deployment:

- `RunWorker(ctx)` takes a Postgres session advisory lock before it touches the
  queue. A second API instance (or the CLI) that cannot get the lock does not
  run jobs, so the 6 s pace holds across processes, not only within one.
- On start it returns jobs left `running` by a dead process to `queued`.
- It claims the oldest `queued` job, runs it, and repeats. When the queue is
  empty it waits for a wake-up from `Ensure` or a periodic poll.
- A **job** gathers one commander: if the commander's colour-identity baseline
  is missing or older than the TTL it samples the baseline first, then samples
  the commander, writing each target when its sample completes. A job's request
  cap is the worst case of the targets it will gather plus a small allowance
  for `429` retries; exceeding it fails the job rather than letting one job
  drift.
- A failed job records its error and the worker waits
  `SYNERGY_FAILURE_COOLDOWN_MINUTES` (default 15) before the next job, so a
  blocked or erroring Archidekt is probed gently instead of hammered. The
  commander returns to `none`; the next `ensure` may queue it again, counting
  against the daily cap.
- Shutdown cancels the context. An interrupted job is left `running` and
  recovered on the next start; a half-fetched target was never written.

### Load bounds

At the defaults a commander costs at most 43 requests, and a new colour
identity's baseline at most 55 more (about 100 requests, ten minutes at 6 s).
With 10 new commanders a day the worst case is roughly an hour of
Archidekt traffic per day, most of it only when many identities are new; a
commander is not fetched again for 14 days. All of it is paced through the one
worker, whatever the number of users.

### Manual CLI

`cmd/synergysync "<commander name>"` resolves the name against the mirror
(case-insensitive exact name, or the front face of a multi-face name; must be
`can_be_commander`), enqueues a job exempt from the daily cap, and drains the
queue through the same worker code. It exits non-zero when the job fails, or
when another process holds the worker lock. It needs only `DATABASE_URL` and
applies migrations first, like `cmd/cardsync`. It exists for development and
for gathering a commander by hand.

## HTTP API

| Endpoint | Method | Query | Response |
|---|---|---|---|
| `/api/synergy/commanders/{cardId}` | GET | `limit` (default 30, max 100) | `CommanderSynergy` |
| `/api/synergy/commanders/{cardId}/ensure` | POST | — | `SynergyStatus` |

`{cardId}` is the commander's Manafold `cards.id`, the id the builder already
holds. Both endpoints sit inside the protected `/api` group, are served from the
local database only, and never call Archidekt.

```json
// POST …/ensure → SynergyStatus
{ "status": "gathering", "reason": null,
  "sample": null }

// GET → CommanderSynergy
{
  "status": "ready",
  "commander": { "card_id": "…", "name": "Krenko, Mob Boss", "color_identity": "R" },
  "sample": { "deck_count": 38, "duplicates_skipped": 2, "fetched_at": "…",
              "baseline_deck_count": 40, "baseline_fetched_at": "…" },
  "cards": [ { "card": CardSummary, "decks": 36, "inclusion": 0.95,
               "baseline": 0.08, "synergy": 0.87 } ],
  "attribution": { "text": "Synergy data from public Archidekt decks",
                   "url": "https://archidekt.com" }
}
```

- `reason` is `disabled`, `daily_cap`, or `not_a_commander` when `status` is
  `none` for a cause the caller can act on, else `null`.
- `sample` is `null` when no sample is stored; `cards` is then empty.
- Cards are ordered by `synergy` descending (nulls last), then inclusion
  descending, then name.
- Only cards that resolve to a mirror row are returned, and that row must be
  commander-legal (`legalities->>'commander' = 'legal'`) and not banned by
  `banlist_overrides`. A card Archidekt users play that the mirror does not
  know, or that is banned, is never surfaced. This holds the HLD's "every card
  shown is a real card from the mirror" goal.
- A `cardId` that names no mirror card returns `404`.
- `baseline_deck_count` / `baseline_fetched_at` are `null` when the identity
  has no baseline yet, and then every `synergy` is `null`.

`Service.Commander(ctx, commanderCardID, limit)` is the Go-level query the GET
handler wraps, and `Service.Ensure` the one the POST handler wraps; the wizard
calls the same functions so its picks carry the same legality filter, status,
and sample metadata.

## Display & Attribution

Wherever synergy data is shown, the surface shall show the line **"Synergy data
from public Archidekt decks"** linking to `https://archidekt.com`, plus the
sample size the numbers come from. This is the attribution Archidekt's
developer asked for. The API response carries the text and link so a surface
cannot forget them. The builder's own panels show no synergy data; the deck
wizard's picks step is the first display (`WIZ-065`) and carries the line and the
sample's deck count.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Synergy source | Co-occurrence over public Archidekt decklists, computed by Manafold | EDHREC (no open API, partnership ruled out); Moxfield (ruled out, ToS-restricted); open data only — `edhrec_rank` + Scryfall oracle tags + Spellbook | Commander-specific co-occurrence is what the wizard needs. The captain accepted the Archidekt ToS risk for a non-monetised personal tool (`MANAFOLD.md` D7). |
| When to gather | On demand: a user building for a commander triggers a queued job | A configured commander list on a weekly batch/cron | The captain changed scope to on-demand. It covers any commander a friend actually builds, with no list to curate. The price is a larger ToS footprint, bounded by the caps below. |
| Request path | `ensure` only enqueues; the worker alone calls Archidekt | Fetch synchronously inside the request | A request must never wait minutes on a rate-limited third party, and per-request fetching would multiply Archidekt traffic by users. |
| Rate limiting | One worker, one client, one 6 s pace, enforced across processes by a Postgres advisory lock | A per-process limiter; a limiter per job | Archidekt sees one polite client however many users and API instances there are. |
| Load caps | 10 newly gathered commanders per rolling 24 h, 40 decks per commander, 14-day TTL, per-job request cap, failure cooldown | Uncapped; per-user caps | Total traffic stays bounded no matter how many users trigger gathering. A global cap is simpler than per-user and is what protects Archidekt. |
| Queue storage | `synergy_jobs` table with a partial unique index | In-memory channel; external queue | It survives restarts, is the audit trail, and makes one-active-job-per-commander a database guarantee. No new infrastructure. |
| Default state | Off (`SYNERGY_ENABLED=true` turns it on) | On by default | A fresh checkout, CI, and tests make no Archidekt request. Turning on user-triggered fetching is a deliberate act. |
| Segment placement | A new `synergy` segment (`SYN`) | Fold into `card-data` (`CARD`); fold into `ai-assist` (`AI`) | `card-data` is the Scryfall mirror and its etiquette and legal footing are clean. Archidekt has a different risk profile, rate limit, and lifetime, and may be switched off wholesale. Keeping it in its own segment keeps that blast radius isolated. `ai-assist` consumes data; it does not crawl. |
| Baseline | Per colour identity: recent decks of the top-ranked *other* commanders in that exact identity, chosen from the mirror | Every sampled deck whose identity is a superset of the card's identity (EDHREC's "decks that could play it"); a colour filter on Archidekt search | Same-identity decks of other commanders is the method the investigation validated, and it needs only the two confirmed endpoints. The superset baseline needs a broad corpus across all identities, which means far more crawling. Archidekt's colour filtering is unconfirmed. |
| Baseline excludes | The commander being gathered and commanders that already have a sample | Exclude only the target; exclude a configured list | One shared baseline per identity stays simple. Excluding already-sampled commanders keeps most commanders' own decks out of the baseline without a list. |
| What is stored | Aggregates only (counts, rates, synergy, sample sizes, fetched-at) plus job bookkeeping | Raw decklists for re-analysis | Minimises what Manafold holds of Archidekt users' content. Raw lists are not needed once tallied. |
| Dedup | Identical tallied card sets within one target's sample count once | Down-weight "Copy of" decks by name; no dedup | Exact clones are the clear failure mode the investigation saw. Name heuristics are fragile, and a lightly edited copy is a real, if correlated, data point. |
| Write granularity | Whole target, one transaction, only after its sample completes | Incremental per-deck counters | Per-deck increments would need persisted deck ids to stay idempotent on resume. A whole-target replace needs no deck ids and never leaves a half-sample visible. |
| Stale data | Served with its fetch time while a refresh is pending | Hide a sample once its TTL passes | A 15-day-old synergy list is far better than none; the status tells the caller whether it is fresh. |
| Card display filter | Mirror rows only, commander-legal, not override-banned | Show Archidekt's names verbatim | Upholds the HLD invariant that every card shown is a real mirror card. |

## Open Questions & Future Decisions

### Deferred
1. **Wizard consumption** (landed, `deck-wizard`). The wizard's plan calls
   `ensure`; its picks step reads `Service.Commander` whenever a sample is stored,
   filtered by colour identity, bracket admission and the player's restrictions, and
   falls back to open-data picks while there is none. Bracket-sliced reads remain
   deferred (item 2).
2. **Bracket-sliced samples.** Archidekt search filters on `edhBracket`, but
   only about 13% of decks declare one. Per-bracket synergy waits until that
   fill rate makes slices meaningful.
3. **Partner pairs.** Only single commanders are sampled.

### Gaps
4. **Endpoint churn.** The search endpoint has moved before (`v3`). A shape
   change surfaces as failed jobs with the decode error, and the last
   aggregates stay in place.
5. **Noise.** About 40 decks per commander gives roughly ±15-point noise on a
   card's synergy. That is good enough to rank picks, not to quote precisely.
6. **Baseline self-overlap.** A commander that is itself one of its identity's
   baseline sources (it was gathered after the baseline was built) sees its own
   signature cards partly offset: its decks are about one fifth of the baseline.
   Accepted; the baseline refreshes after the TTL and then excludes it.
7. **User-triggered ToS exposure.** Gathering follows user activity rather than
   a fixed schedule. The caps bound it, but the first sign of trouble from
   Archidekt (a block, a lockdown) should switch `SYNERGY_ENABLED` off.

## References

- Code: `backend/internal/synergy/` (`Service`, `Ensure`, `RunWorker`, the
  Archidekt client, `tally`), `backend/cmd/synergysync/main.go`,
  `backend/internal/api/synergy.go`,
  `backend/internal/db/migrations/000009_create_synergy.up.sql`,
  `backend/internal/db/queries/synergy.sql`
- Test fixtures: `backend/internal/synergy/testdata/` (Archidekt-shaped search
  and deck responses, trimmed, with owner data removed)
- Decision: `MANAFOLD.md` D7 (Archidekt option 1, on-demand gathering, and the
  accepted risk)
- Archidekt: <https://archidekt.com>, Terms <https://archidekt.com/terms>,
  developer statement <https://archidekt.com/forum/thread/40353>
