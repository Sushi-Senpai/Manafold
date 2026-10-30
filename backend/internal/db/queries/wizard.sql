-- Candidate pools for the deck wizard (WIZ-040, WIZ-041, WIZ-043, WIZ-044).
-- Every pool is the same legality definition as the rest of the product: inside
-- the deck's colour identity, commander-legal after banlist_overrides, and not
-- one of the excluded cards (the deck's own cards and the player's rejections).
-- "Land" always means the front face: a modal double-faced "Instant // Land" is a
-- spell here.

-- Nonbasic lands that make at least two of the deck's colours, plus fetch lands
-- when include_fetch is set: a land whose Oracle text sacrifices itself to search
-- the library (which also covers Evolving Wilds-style lands), or one carrying the
-- fetchland tag or a descendant. The text rule keeps the category alive if the
-- community tag is renamed or thin.
-- @spec WIZ-040
-- name: ListWizardFixingLands :many
SELECT c.* FROM cards c
LEFT JOIN banlist_overrides bo ON lower(bo.card_name) = lower(c.name)
WHERE split_part(c.type_line, ' // ', 1) LIKE '%Land%'
  AND split_part(c.type_line, ' // ', 1) NOT LIKE '%Basic%'
  AND c.color_identity <@ sqlc.arg(identity)::text[]
  AND c.id <> ALL(sqlc.arg(exclude_ids)::uuid[])
  AND CASE WHEN bo.card_name IS NOT NULL THEN NOT bo.banned
           ELSE c.legalities->>'commander' = 'legal' END
  AND (
    (SELECT count(DISTINCT pm.x) FROM unnest(c.produced_mana) AS pm(x)
      WHERE pm.x = ANY(sqlc.arg(identity)::text[])) >= 2
    OR (sqlc.arg(include_fetch)::bool AND (
        c.oracle_text ~* 'sacrifice [^:]*: search your library for a'
        OR EXISTS (
            SELECT 1 FROM card_oracle_tags ct
            WHERE ct.card_id = c.id
              AND ct.tag_id IN (SELECT oracle_tag_reach('fetchland')))))
  )
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT sqlc.arg(lim);

-- Lands carrying the utility-land tag or a descendant.
-- @spec WIZ-041
-- name: ListWizardUtilityLands :many
SELECT c.* FROM cards c
LEFT JOIN banlist_overrides bo ON lower(bo.card_name) = lower(c.name)
WHERE split_part(c.type_line, ' // ', 1) LIKE '%Land%'
  AND split_part(c.type_line, ' // ', 1) NOT LIKE '%Basic%'
  AND c.color_identity <@ sqlc.arg(identity)::text[]
  AND c.id <> ALL(sqlc.arg(exclude_ids)::uuid[])
  AND CASE WHEN bo.card_name IS NOT NULL THEN NOT bo.banned
           ELSE c.legalities->>'commander' = 'legal' END
  AND EXISTS (
    SELECT 1 FROM card_oracle_tags ct
    WHERE ct.card_id = c.id
      AND ct.tag_id IN (SELECT oracle_tag_reach('utility-land')))
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT sqlc.arg(lim);

-- Nonland cards by edhrec_rank: the picks pool while a commander has no stored
-- synergy sample.
-- @spec WIZ-043
-- name: ListWizardPopularNonlands :many
SELECT c.* FROM cards c
LEFT JOIN banlist_overrides bo ON lower(bo.card_name) = lower(c.name)
WHERE split_part(c.type_line, ' // ', 1) NOT LIKE '%Land%'
  AND c.edhrec_rank IS NOT NULL
  AND c.color_identity <@ sqlc.arg(identity)::text[]
  AND c.id <> ALL(sqlc.arg(exclude_ids)::uuid[])
  AND CASE WHEN bo.card_name IS NOT NULL THEN NOT bo.banned
           ELSE c.legalities->>'commander' = 'legal' END
ORDER BY c.edhrec_rank ASC, c.name ASC
LIMIT sqlc.arg(lim);

-- Nonland Game Changers within the identity, best-ranked first.
-- @spec WIZ-044
-- name: ListWizardGameChangers :many
SELECT c.* FROM cards c
LEFT JOIN banlist_overrides bo ON lower(bo.card_name) = lower(c.name)
WHERE c.is_game_changer
  AND split_part(c.type_line, ' // ', 1) NOT LIKE '%Land%'
  AND c.color_identity <@ sqlc.arg(identity)::text[]
  AND c.id <> ALL(sqlc.arg(exclude_ids)::uuid[])
  AND CASE WHEN bo.card_name IS NOT NULL THEN NOT bo.banned
           ELSE c.legalities->>'commander' = 'legal' END
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT sqlc.arg(lim);

-- The lowest known USD price of each card across its non-digital printings; a
-- card with no priced printing is absent from the result (WIZ-031).
-- @spec WIZ-031
-- name: ListCheapestUSD :many
SELECT cp.card_id, MIN((cp.prices->>'usd')::float8)::float8 AS usd
FROM card_prints cp
WHERE cp.card_id = ANY(sqlc.arg(card_ids)::uuid[])
  AND NOT cp.is_digital
  AND cp.prices->>'usd' IS NOT NULL
GROUP BY cp.card_id;

-- The deck wizard's saved answers (WIZ-092). Callers check deck ownership first.
-- @spec WIZ-100
-- name: GetWizardState :one
SELECT state, updated_at FROM deck_wizard_state WHERE deck_id = $1;

-- @spec WIZ-101
-- name: UpsertWizardState :one
INSERT INTO deck_wizard_state (deck_id, state) VALUES ($1, $2)
ON CONFLICT (deck_id) DO UPDATE SET state = EXCLUDED.state, updated_at = now()
RETURNING state, updated_at;

-- @spec WIZ-103
-- name: DeleteWizardState :exec
DELETE FROM deck_wizard_state WHERE deck_id = $1;
