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
deck-building wizard is the main consumer. The builder shows a minimal synergy
panel.

**Synergy** for a card *c* under commander *K* is

```
synergy(c, K) = inclusion(c among K's decks) − inclusion(c among baseline decks for K's colour identity)
```

where an inclusion rate is the fraction of sampled decks that contain the card.
A staple every deck plays (Sol Ring) scores near zero. A card mostly played with
*K* (Goblin Warchief under Krenko) scores high. Another commander's signature
payoff scores negative. This is the same shape EDHREC publishes; EDHREC has no
open API, which is why Manafold computes it itself.

Archidekt has no documented API, but its public read endpoints are open with no
auth. Archidekt's developer said in 2019 that the read API is open and public,
asked for a link back, and warned they may lock it down if traffic becomes a
burden. Their Terms of Use are stricter: personal non-commercial use, no
automated queries. Manafold is a personal, non-monetised tool for the captain
and friends, and the captain accepted that risk (see `MANAFOLD.md` decision D7).
Every choice in this segment keeps the footprint small and polite: a
server-side batch job, never a client request path; one request every 6 s at
most; popular commanders only; only aggregates stored; attribution wherever the
data appears. The design also assumes the source **can disappear without
notice**: everything here is additive, and a lockdown leaves the last
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
  Partner pairs are out of scope: each configured commander is sampled on its
  own.

### HTTP etiquette

- **User-Agent** `Manafold-synergy/1.0 (+https://github.com/Sushi-Senpai/Manafold)`
  plus `Accept: application/json`. The User-Agent identifies the project and
  contains no personal data.
- **Pacing.** Successive requests start at least **6 s** apart (the
  investigation saw no `429` at that pace; 2 s tripped it within about ten
  requests).
- **429 back-off.** Archidekt sends no `Retry-After`. On a `429` the job waits
  an escalating back-off (2 min, then 4 min, then 8 min) before retrying the
  same request. A fourth consecutive `429` stops the run.
- **Bounded request phases.** Each request runs under a per-request timeout (a
  deck detail is a few hundred KB), so a stalled connection cannot hang a cron
  run.
- A deck that answers `403` / `404` / `410` between search and fetch (made
  private or deleted) is skipped and counted. Any other non-2xx stops the run.

## Sampling

A **target** is one unit of refresh work.

- **Commander target**, one per configured commander. It samples up to
  `SYNERGY_DECKS_PER_COMMANDER` (default 60) of that commander's most recent
  public decks, reading at most `SYNERGY_SEARCH_PAGES` (default 3) search pages
  to find them.
- **Baseline target**, one per distinct colour identity among the configured
  commanders. It samples `SYNERGY_DECKS_PER_BASELINE_COMMANDER` (default 12)
  recent decks from each of the `SYNERGY_BASELINE_COMMANDERS` (default 5)
  highest-ranked *other* commanders in that identity. Those are chosen from the
  mirror: `can_be_commander`, commander-legal, commander colour identity
  exactly equal to the target identity, not themselves a configured commander,
  ordered by `edhrec_rank` (nulls last), then name. Excluding every configured
  commander keeps any commander's own decks out of its baseline. An identity
  with no such commander in the mirror gets an empty baseline.

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
set is skipped as unusable.

## Storage

Migration `create_synergy`. Only aggregates are stored. Decklists, deck ids,
deck names, owners, and any other user data are never persisted; a sample
lives in memory only while its target is being refreshed.

### `synergy_commanders`

| column | type | notes |
|---|---|---|
| `commander_oracle_id` | uuid pk, fk → `cards(scryfall_oracle_id)` on delete cascade | |
| `commander_name` | text not null | the name as configured / resolved |
| `color_identity` | text not null | canonical WUBRG-ordered letters; `C` for colourless |
| `deck_count` | int not null | decks counted (after dedup) |
| `duplicates_skipped` | int not null | |
| `fetched_at` | timestamptz not null | when the sample was taken |

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

### `synergy_runs`

`id`, `status` (`running` / `succeeded` / `failed`), `requests_made`,
`targets_refreshed`, `targets_pending`, `started_at`, `finished_at`, `error`.
An audit row per run, like `sync_runs`.

### Write path

A target's aggregates are written in **one transaction, only after its whole
sample has been fetched**:

1. Upsert the header row.
2. Delete the target's old card rows and `COPY` the new ones.
3. Recompute `baseline_rate` / `synergy` for every commander in the affected
   colour identity.

A run that dies mid-target leaves that target's previous aggregates intact.
Recomputing on both sides (after a commander write and after a baseline write)
means synergy is never left computed against a stale baseline, whatever order
the targets refresh in.

## Batch Job (`cmd/synergysync`, `internal/synergy`)

`synergy.Run(ctx, pool, opts)` is the entry point. `cmd/synergysync` calls it;
it needs only `DATABASE_URL` (`config.LoadCardsync`'s fail-fast) and applies
migrations first, like `cmd/cardsync`.

1. **Commander list.** One commander name per line; blank lines and `#`
   comments are ignored. The embedded default
   (`internal/synergy/commanders.txt`, a modest list of popular commanders
   spread across colour identities) is used unless `SYNERGY_COMMANDERS_FILE`
   names another file. Each name resolves against `cards` by case-insensitive
   exact name, or by the front face of a multi-face name, and must be
   `can_be_commander`. An unresolved name is logged and skipped; it never
   fails the run.
2. **Plan.** Build the commander targets and one baseline target per distinct
   commander colour identity. Keep only the **stale** ones: never fetched, or
   `fetched_at` older than `SYNERGY_REFRESH_DAYS` (default 7). Order them
   stalest first; never-fetched ones go first, baselines before commanders.
3. **Bounded.** A run makes at most `SYNERGY_MAX_REQUESTS` (default 600, about
   an hour at the 6 s pace) Archidekt requests. Before starting a target, the
   run checks that the remaining budget covers that target's worst-case
   request count. If it does not, the run stops and records the remaining
   stale targets as `targets_pending`. No target is ever cut off by the budget
   halfway through.
4. **Resumable.** Progress lives in the targets' `fetched_at`. Whether the
   previous run ran out of budget, failed, or was killed, the next run picks up
   exactly the targets still stale. Nothing is re-fetched that was already
   refreshed within the window.
5. Record the `synergy_runs` row. A run that stops on the budget is
   `succeeded`, with pending targets counted. A run that stops on `429`
   exhaustion or any other error is `failed`, with the error text, and the
   process exits non-zero for the scheduler to surface. Targets completed
   before the failure keep their new aggregates.

**Scheduling.** The job is built into the backend image as `/app/synergysync`.
It can be run by hand (`go run ./cmd/synergysync` from `backend/`) or as a cron.
With the default list (about a dozen commanders plus their baselines), one full
refresh is roughly 1,500 requests. A daily cron at the default budget refreshes
every target about weekly, and a weekly cron catches up over a few runs.
Turning on a deployed schedule is an operations step, not part of this segment.

## HTTP API

| Endpoint | Method | Query | Response |
|---|---|---|---|
| `/api/synergy/commanders/{cardId}` | GET | `limit` (default 30, max 100) | `CommanderSynergy` |

`{cardId}` is the commander's Manafold `cards.id`, the id the builder already
holds. The endpoint sits inside the protected `/api` group, served from the
local database only. It never calls Archidekt on a request path.

```json
{
  "commander": { "card_id": "…", "name": "Krenko, Mob Boss", "color_identity": "R" },
  "sample": { "deck_count": 58, "duplicates_skipped": 2, "fetched_at": "…",
              "baseline_deck_count": 60, "baseline_fetched_at": "…" },
  "cards": [ { "card": CardSummary, "decks": 55, "inclusion": 0.95,
               "baseline": 0.08, "synergy": 0.87 } ],
  "attribution": { "text": "Synergy data from public Archidekt decks",
                   "url": "https://archidekt.com" }
}
```

- Cards are ordered by `synergy` descending (nulls last), then inclusion
  descending, then name.
- Only cards that resolve to a mirror row are returned, and that row must be
  commander-legal (`legalities->>'commander' = 'legal'`) and not banned by
  `banlist_overrides`. A card Archidekt users play that the mirror does not
  know, or that is banned, is never surfaced. This holds the HLD's "every card
  shown is a real card from the mirror" goal.
- A commander with no sample returns `404`.
- `baseline_deck_count` / `baseline_fetched_at` are `null` when the identity
  has no baseline yet, and then every `synergy` is `null`.

`synergy.TopCards(ctx, q, commanderCardID, limit)` is the Go-level query the
handler wraps. The wizard calls the same function so its picks carry the same
legality filter and sample metadata.

## Display & Attribution

Wherever synergy data is shown, the surface shows the line **"Synergy data from
public Archidekt decks"** linking to `https://archidekt.com`, plus the sample
size the numbers come from. This is the attribution Archidekt's developer
asked for.

The builder's minimal surface is a **Synergy panel** in the right column under
AI suggestions, shown once the deck has a commander. It lists the top cards
with synergy and inclusion as whole percentages and an *Add* button. Cards
already on the deck's main or command board show *In deck* instead. A footer
gives the deck count, the fetch date, and the attribution link. When the
commander has no data, the panel says so.

## Decisions & Alternatives

| Decision | Chosen | Alternatives Considered | Rationale |
|---|---|---|---|
| Synergy source | Co-occurrence over public Archidekt decklists, computed by Manafold | EDHREC (no open API, partnership ruled out); Moxfield (ruled out, ToS-restricted); open data only — `edhrec_rank` + Scryfall oracle tags + Spellbook | Commander-specific co-occurrence is what the wizard needs. The captain accepted the Archidekt ToS risk for a non-monetised personal tool (`MANAFOLD.md` D7). |
| Segment placement | A new `synergy` segment (`SYN`) | Fold into `card-data` (`CARD`); fold into `ai-assist` (`AI`) | `card-data` is the Scryfall mirror and its etiquette and legal footing are clean. Archidekt has a different risk profile, rate limit, and lifetime, and may be switched off wholesale. Keeping it in its own segment keeps that blast radius isolated. `ai-assist` consumes data; it does not crawl. |
| Baseline | Per colour identity: recent decks of the top-ranked *other* commanders in that exact identity, chosen from the mirror | Every sampled deck whose identity is a superset of the card's identity (EDHREC's "decks that could play it"); a colour filter on Archidekt search | Same-identity decks of other commanders is the method the investigation validated, and it needs only the two confirmed endpoints. The superset baseline needs a broad corpus across all identities, which means far more crawling. Archidekt's colour filtering is unconfirmed. |
| Baseline excludes configured commanders | Yes | Exclude only the target commander per query (store per-baseline-commander slices) | One shared baseline per identity stays simple and is never contaminated by any configured commander's own decks. |
| What is stored | Aggregates only (counts, rates, synergy, sample sizes, fetched-at) | Raw decklists for re-analysis | Minimises what Manafold holds of Archidekt users' content. Raw lists are not needed once tallied. |
| Dedup | Identical tallied card sets within one target's sample count once | Down-weight "Copy of" decks by name; no dedup | Exact clones are the clear failure mode the investigation saw. Name heuristics are fragile, and a lightly edited copy is a real, if correlated, data point. |
| Write granularity | Whole target, one transaction, only after its sample completes | Incremental per-deck counters | Per-deck increments would need persisted deck ids to stay idempotent on resume. A whole-target replace needs no deck ids and never leaves a half-sample visible. |
| Budget enforcement | Start a target only if its worst case fits the remaining budget | Stop mid-target when the budget hits zero | Avoids throwing away fetched work, and keeps "bounded" a hard upper limit on requests. |
| Pacing | 6 s minimum spacing; escalating back-off on `429`, no `Retry-After` | Faster pacing with reactive back-off | 6 s produced no `429` in the investigation's sample. Archidekt sends no `Retry-After`, and burdening their servers is what would trigger a lockdown. |
| Card display filter | Mirror rows only, commander-legal, not override-banned | Show Archidekt's names verbatim | Upholds the HLD invariant that every card shown is a real mirror card. |

## Open Questions & Future Decisions

### Deferred
1. **Wizard consumption.** The deck-building wizard reads `TopCards` for its
   synergy step, filtered further by bracket and the user's choices. That
   wiring belongs to the wizard's own design.
2. **Bracket-sliced samples.** Archidekt search filters on `edhBracket`, but
   only about 13% of decks declare one. Per-bracket synergy waits until that
   fill rate makes slices meaningful.
3. **Partner pairs and long-tail commanders.** Only single, configured
   commanders are sampled. On-demand sampling of an arbitrary commander would
   put Archidekt calls closer to a user action, so it is deliberately not
   built.
4. **Deployed schedule.** A Render cron entry for `/app/synergysync` is an
   operations choice, made when the captain decides to turn the crawl on in a
   deployed environment.

### Gaps
5. **Endpoint churn.** The search endpoint has moved before (`v3`). A shape
   change surfaces as a failed run with the decode error, and the last
   aggregates stay in place.
6. **Noise.** About 60 decks per commander gives roughly ±12-point noise on a
   card's synergy. That is good enough to rank picks, not to quote precisely.

## References

- Code: `backend/internal/synergy/` (`Run`, the Archidekt client, `tally`,
  `TopCards`, `commanders.txt`), `backend/cmd/synergysync/main.go`,
  `backend/internal/api/synergy.go`,
  `backend/internal/db/migrations/000009_create_synergy.up.sql`,
  `backend/internal/db/queries/synergy.sql`,
  `frontend/src/components/builder/SynergyPanel.tsx`,
  `frontend/src/lib/synergy.ts`
- Test fixtures: `backend/internal/synergy/testdata/` (Archidekt-shaped search
  and deck responses, trimmed, with owner data removed)
- Decision: `MANAFOLD.md` D7 (Archidekt option 1 and its accepted risk)
- Archidekt: <https://archidekt.com>, Terms <https://archidekt.com/terms>,
  developer statement <https://archidekt.com/forum/thread/40353>
