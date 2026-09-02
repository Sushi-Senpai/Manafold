# Card Data — EARS Specs

## Sync Job

- [x] **CARD-001**: When the card-sync job runs, the system shall download the current Scryfall `oracle_cards` and `default_cards` bulk exports, upsert one `cards` row per Scryfall `oracle_id` and one `card_prints` row per Scryfall printing `id`, and record a `sync_runs` row carrying the source `updated_at`, the upserted row count, and a terminal status.
- [x] **CARD-002**: The system shall store each card's `color_identity` exactly as provided by Scryfall and shall never recompute it.
- [x] **CARD-003**: When ingesting a card, the system shall set `singleton_limit` to `0` if the card's Oracle text states a deck may contain any number of cards with that name, to the stated numeric cap if the text states an "up to N" limit, and to `NULL` (normal singleton) otherwise.
- [x] **CARD-004**: When ingesting a card, the system shall set `can_be_commander` to true if the card's type line contains both "Legendary" and "Creature" or its Oracle text (on any face) contains "can be your commander", and false otherwise.
- [x] **CARD-005**: When ingesting a `default_cards` printing whose `oracle_id` has no matching `cards` row, the system shall skip that printing and count it, rather than failing the run.
- [x] **CARD-006**: Every outbound Scryfall request the sync job makes shall carry a descriptive `User-Agent` identifying Manafold and an explicit `Accept` header, and on an HTTP `429` the job shall wait at least 30 seconds before one retry.
- [x] **CARD-007**: If any step of a bulk ingestion fails, then the system shall mark that `sync_runs` row `failed` with the error text and a `finished_at`, and the job process shall exit non-zero.
- [x] **CARD-008**: The system shall not call any Scryfall `/cards/*` endpoint on a client-facing request path; live Scryfall calls occur only in the sync job and (from M2) an explicit single-printing fallback on card add.
- [ ] **CARD-009**: When a client adds a card by a collector number not present in `card_prints`, the system shall fetch that single printing from Scryfall's `/cards/collection` endpoint (held under ~2 requests/second), upsert it, and then complete the add.
- [D] **CARD-010**: When the card-sync job runs, the system shall also ingest Scryfall's `oracle_tags` bulk file to seed `deck-building`'s functional auto-categorizer.
- [D] **CARD-011**: The system shall mirror non-English printings from the Scryfall `all_cards` export.

## Search & Autocomplete

- [x] **CARD-020**: When a client calls `GET /api/cards/autocomplete?q=<prefix>`, the system shall return up to 20 card names whose name begins with the prefix (case-insensitively), ordered by ascending `edhrec_rank` with unranked cards last, and shall return an empty list for an empty prefix.
- [x] **CARD-021**: When a client calls `GET /api/cards/search?q=<query>`, the system shall return a page of matching Oracle-level cards from the local mirror, each carrying the card's Oracle fields and its newest printing's image and price data.
- [x] **CARD-022**: The `GET /api/cards/search` query language shall support, over the mirror, bare full-text terms and the `id:` / `id=` / `id>=` colour-identity, `t:` type-line, `cmc` mana-value comparison, `o:` Oracle-text, and `is:commander` predicates.
- [x] **CARD-023**: When a `GET /api/cards/search` query string cannot be parsed, the system shall respond `400` with a message naming the offending token, rather than silently returning unfiltered or empty results.
- [x] **CARD-024**: The system shall serve every `GET /api/cards/*` response from the local mirror only, performing no Scryfall call on that request path (reinforces CARD-008).

## Banlist Overrides

- [x] **CARD-030**: The system shall provide a `banlist_overrides` table keyed by card name with a `banned` boolean, which `deck-building`'s legality validator consults after `legalities->>'commander'` so a Panel announcement can be reflected before the next Scryfall refresh.
