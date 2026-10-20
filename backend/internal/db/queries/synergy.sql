-- Resolve a configured commander name to its cards row: an exact
-- case-insensitive match on the whole name, or on the front face of a
-- multi-face card, and only a card that can be a commander (SYN-001).
-- @spec SYN-001
-- name: ResolveSynergyCommander :one
SELECT scryfall_oracle_id, name,
       COALESCE(commander_color_identity, color_identity)::text[] AS identity
FROM cards
WHERE can_be_commander
  AND (lower(name) = lower(sqlc.arg(name)::text)
       OR lower(name) LIKE lower(sqlc.arg(name)::text) || ' // %')
ORDER BY (lower(name) = lower(sqlc.arg(name)::text)) DESC,
         edhrec_rank ASC NULLS LAST
LIMIT 1;

-- One card as the synergy trigger sees it: its oracle id, name, colour
-- identity, and whether it may be gathered — a commander that is legal in
-- Commander, with banlist_overrides taking precedence exactly as in deck
-- validation (SYN-030, SYN-042).
-- @spec SYN-030, SYN-042
-- name: GetSynergyCommanderCard :one
SELECT c.scryfall_oracle_id, c.name,
       COALESCE(c.commander_color_identity, c.color_identity)::text[] AS identity,
       (c.can_be_commander
        AND CASE WHEN bo.card_name IS NOT NULL THEN NOT bo.banned
                 ELSE c.legalities->>'commander' = 'legal' END)::boolean AS gatherable
FROM cards c
LEFT JOIN banlist_overrides bo ON lower(bo.card_name) = lower(c.name)
WHERE c.id = $1;

-- The baseline commanders for one colour identity: the highest-ranked
-- commander-legal cards that can be a commander with exactly that commander
-- colour identity, excluding the commander being gathered and every commander
-- that already has a stored sample (SYN-011).
-- @spec SYN-011
-- name: ListBaselineCommanders :many
SELECT scryfall_oracle_id, name
FROM cards
WHERE can_be_commander
  AND legalities->>'commander' = 'legal'
  AND COALESCE(commander_color_identity, color_identity) @> sqlc.arg(identity)::text[]
  AND COALESCE(commander_color_identity, color_identity) <@ sqlc.arg(identity)::text[]
  AND scryfall_oracle_id <> sqlc.arg(exclude)::uuid
  AND scryfall_oracle_id NOT IN (SELECT commander_oracle_id FROM synergy_commanders)
ORDER BY edhrec_rank ASC NULLS LAST, name ASC
LIMIT sqlc.arg(lim);

-- The stored baseline for one colour identity, if any, to decide whether it is
-- still fresh (SYN-034).
-- @spec SYN-034
-- name: GetSynergyBaselineFreshness :one
SELECT fetched_at FROM synergy_baselines WHERE color_identity = $1;

-- @spec SYN-020, SYN-022
-- name: UpsertSynergyCommander :exec
INSERT INTO synergy_commanders (
    commander_oracle_id, commander_name, color_identity, deck_count, duplicates_skipped, fetched_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (commander_oracle_id) DO UPDATE SET
    commander_name     = EXCLUDED.commander_name,
    color_identity     = EXCLUDED.color_identity,
    deck_count         = EXCLUDED.deck_count,
    duplicates_skipped = EXCLUDED.duplicates_skipped,
    fetched_at         = EXCLUDED.fetched_at;

-- name: DeleteSynergyCommanderCards :exec
DELETE FROM synergy_commander_cards WHERE commander_oracle_id = $1;

-- name: InsertSynergyCommanderCards :copyfrom
INSERT INTO synergy_commander_cards (
    commander_oracle_id, card_oracle_id, deck_count, inclusion_rate
) VALUES ($1, $2, $3, $4);

-- @spec SYN-020, SYN-022
-- name: UpsertSynergyBaseline :exec
INSERT INTO synergy_baselines (
    color_identity, baseline_commanders, deck_count, duplicates_skipped, fetched_at
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (color_identity) DO UPDATE SET
    baseline_commanders = EXCLUDED.baseline_commanders,
    deck_count          = EXCLUDED.deck_count,
    duplicates_skipped  = EXCLUDED.duplicates_skipped,
    fetched_at          = EXCLUDED.fetched_at;

-- name: DeleteSynergyBaselineCards :exec
DELETE FROM synergy_baseline_cards WHERE color_identity = $1;

-- name: InsertSynergyBaselineCards :copyfrom
INSERT INTO synergy_baseline_cards (
    color_identity, card_oracle_id, deck_count, inclusion_rate
) VALUES ($1, $2, $3, $4);

-- Recompute baseline_rate and synergy for every commander in one colour
-- identity: synergy = inclusion − baseline inclusion, a card missing from a
-- non-empty baseline counting as 0, both NULL while the identity has no
-- baseline sample (SYN-021). Run after either side of the pair is written.
-- @spec SYN-021
-- name: RecomputeSynergyForIdentity :exec
UPDATE synergy_commander_cards cc
SET baseline_rate = s.rate,
    synergy       = cc.inclusion_rate - s.rate
FROM (
    SELECT cc2.commander_oracle_id, cc2.card_oracle_id,
           CASE WHEN b.deck_count > 0 THEN COALESCE(bc.inclusion_rate, 0) END AS rate
    FROM synergy_commander_cards cc2
    JOIN synergy_commanders c ON c.commander_oracle_id = cc2.commander_oracle_id
    LEFT JOIN synergy_baselines b ON b.color_identity = c.color_identity
    LEFT JOIN synergy_baseline_cards bc
           ON bc.color_identity = c.color_identity AND bc.card_oracle_id = cc2.card_oracle_id
    WHERE c.color_identity = sqlc.arg(color_identity)::text
) s
WHERE cc.commander_oracle_id = s.commander_oracle_id
  AND cc.card_oracle_id = s.card_oracle_id;

-- Queue a gathering job for a commander unless one is already queued or
-- running; no row comes back when one is (SYN-031).
-- @spec SYN-030, SYN-031
-- name: EnqueueSynergyJob :one
INSERT INTO synergy_jobs (commander_oracle_id, status)
VALUES ($1, 'queued')
ON CONFLICT (commander_oracle_id) WHERE status IN ('queued', 'running') DO NOTHING
RETURNING id;

-- @spec SYN-040
-- name: HasActiveSynergyJob :one
SELECT EXISTS (
    SELECT 1 FROM synergy_jobs
    WHERE commander_oracle_id = $1 AND status IN ('queued', 'running')
)::boolean;

-- Jobs requested since a point in time, for the rolling daily cap (SYN-032).
-- @spec SYN-032
-- name: CountSynergyJobsSince :one
SELECT count(*)::int FROM synergy_jobs WHERE requested_at > $1;

-- Take the oldest queued job and mark it running (SYN-033).
-- @spec SYN-033
-- name: ClaimNextSynergyJob :one
UPDATE synergy_jobs
SET status = 'running', started_at = now()
WHERE id = (
    SELECT id FROM synergy_jobs WHERE status = 'queued'
    ORDER BY requested_at, id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, commander_oracle_id;

-- @spec SYN-035
-- name: FinishSynergyJob :exec
UPDATE synergy_jobs
SET status = $2, requests_made = $3, error = $4, finished_at = now()
WHERE id = $1;

-- Jobs a dead process left running go back to the queue (SYN-035).
-- @spec SYN-035
-- name: RequeueRunningSynergyJobs :exec
UPDATE synergy_jobs SET status = 'queued', started_at = NULL WHERE status = 'running';

-- The sample behind one commander's synergy, looked up by the commander's
-- Manafold card id, with its colour identity's baseline if there is one
-- (SYN-040, SYN-042).
-- @spec SYN-040, SYN-042
-- name: GetSynergySample :one
SELECT sc.commander_oracle_id, sc.commander_name, sc.color_identity,
       sc.deck_count, sc.duplicates_skipped, sc.fetched_at,
       b.deck_count AS baseline_deck_count, b.fetched_at AS baseline_fetched_at
FROM cards c
JOIN synergy_commanders sc ON sc.commander_oracle_id = c.scryfall_oracle_id
LEFT JOIN synergy_baselines b ON b.color_identity = sc.color_identity
WHERE c.id = $1;

-- One commander's top synergy cards, each joined to its mirror row and newest
-- printing. Only commander-legal cards are returned, with a banlist_overrides
-- row taking precedence exactly as in deck validation; a card the mirror does
-- not know drops out of the join (SYN-040, SYN-041).
-- @spec SYN-040, SYN-041
-- name: ListTopSynergyCards :many
SELECT sqlc.embed(c),
       sc.deck_count, sc.inclusion_rate, sc.baseline_rate, sc.synergy,
       p.image_uris, p.prices,
       COALESCE(p.set_code, '')::text AS set_code,
       COALESCE(p.collector_number, '')::text AS collector_number
FROM synergy_commander_cards sc
JOIN cards c ON c.scryfall_oracle_id = sc.card_oracle_id
LEFT JOIN banlist_overrides bo ON lower(bo.card_name) = lower(c.name)
LEFT JOIN LATERAL (
    SELECT cp.image_uris, cp.prices, cp.set_code, cp.collector_number
    FROM card_prints cp
    WHERE cp.card_id = c.id
    ORDER BY cp.released_at DESC NULLS LAST
    LIMIT 1
) p ON true
WHERE sc.commander_oracle_id = sqlc.arg(commander_oracle_id)
  AND CASE WHEN bo.card_name IS NOT NULL THEN NOT bo.banned
           ELSE c.legalities->>'commander' = 'legal' END
ORDER BY sc.synergy DESC NULLS LAST, sc.inclusion_rate DESC, c.name ASC
LIMIT sqlc.arg(lim);
