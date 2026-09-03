-- name: GetCardByID :one
SELECT * FROM cards WHERE id = $1;

-- name: GetCardByScryfallOracleID :one
SELECT * FROM cards WHERE scryfall_oracle_id = $1;

-- name: GetCardsByIDs :many
SELECT * FROM cards WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- Up to 20 name completions for a prefix, best (lowest edhrec_rank) first.
-- @spec CARD-020
-- name: AutocompleteCardNames :many
SELECT name
FROM cards
WHERE lower(name) LIKE lower(sqlc.arg(prefix)) || '%'
ORDER BY edhrec_rank ASC NULLS LAST, name ASC
LIMIT 20;

-- The newest printing for a card — the default display printing when a
-- deck_cards entry has no chosen print_id.
-- name: GetNewestPrintForCard :one
SELECT * FROM card_prints
WHERE card_id = $1
ORDER BY released_at DESC NULLS LAST
LIMIT 1;

-- name: GetPrintByID :one
SELECT * FROM card_prints WHERE id = $1;

-- Resolve a decklist line's card name to one cards row: an exact
-- case-insensitive match on the whole name, or a match on one face of a
-- split / double-faced card ("Fire" for "Fire // Ice"), preferring the exact
-- whole-name match (PORT-005).
-- @spec PORT-005
-- name: ResolveCardByName :one
SELECT * FROM cards
WHERE lower(name) = lower(sqlc.arg(name)::text)
   OR lower(name) LIKE lower(sqlc.arg(name)::text) || ' // %'
   OR lower(name) LIKE '% // ' || lower(sqlc.arg(name)::text)
ORDER BY (lower(name) = lower(sqlc.arg(name)::text)) DESC,
         edhrec_rank ASC NULLS LAST
LIMIT 1;

-- @spec CARD-030
-- name: ListBanlistOverrides :many
SELECT card_name, banned FROM banlist_overrides;

-- @spec CARD-001
-- name: UpsertCard :one
INSERT INTO cards (
    scryfall_oracle_id, name, mana_cost, mana_value, type_line, oracle_text,
    colors, color_identity, produced_mana, keywords, power, toughness, loyalty,
    legalities, is_game_changer, is_reserved, layout, card_faces,
    singleton_limit, can_be_commander, commander_color_identity, edhrec_rank
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
    $18, $19, $20, $21, $22
)
ON CONFLICT (scryfall_oracle_id) DO UPDATE SET
    name = EXCLUDED.name,
    mana_cost = EXCLUDED.mana_cost,
    mana_value = EXCLUDED.mana_value,
    type_line = EXCLUDED.type_line,
    oracle_text = EXCLUDED.oracle_text,
    colors = EXCLUDED.colors,
    color_identity = EXCLUDED.color_identity,
    produced_mana = EXCLUDED.produced_mana,
    keywords = EXCLUDED.keywords,
    power = EXCLUDED.power,
    toughness = EXCLUDED.toughness,
    loyalty = EXCLUDED.loyalty,
    legalities = EXCLUDED.legalities,
    is_game_changer = EXCLUDED.is_game_changer,
    is_reserved = EXCLUDED.is_reserved,
    layout = EXCLUDED.layout,
    card_faces = EXCLUDED.card_faces,
    singleton_limit = EXCLUDED.singleton_limit,
    can_be_commander = EXCLUDED.can_be_commander,
    commander_color_identity = EXCLUDED.commander_color_identity,
    edhrec_rank = EXCLUDED.edhrec_rank
RETURNING *;

-- @spec CARD-001
-- name: UpsertCardPrint :one
INSERT INTO card_prints (
    scryfall_id, card_id, set_code, set_name, collector_number, rarity,
    released_at, finishes, image_uris, prices, is_promo, is_reprint, is_digital
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (scryfall_id) DO UPDATE SET
    card_id = EXCLUDED.card_id,
    set_code = EXCLUDED.set_code,
    set_name = EXCLUDED.set_name,
    collector_number = EXCLUDED.collector_number,
    rarity = EXCLUDED.rarity,
    released_at = EXCLUDED.released_at,
    finishes = EXCLUDED.finishes,
    image_uris = EXCLUDED.image_uris,
    prices = EXCLUDED.prices,
    is_promo = EXCLUDED.is_promo,
    is_reprint = EXCLUDED.is_reprint,
    is_digital = EXCLUDED.is_digital
RETURNING *;

-- @spec CARD-001, CARD-007
-- name: CreateSyncRun :one
INSERT INTO sync_runs (bulk_type, scryfall_updated_at, status)
VALUES ($1, $2, 'running')
RETURNING *;

-- name: FinishSyncRun :exec
UPDATE sync_runs
SET status = 'succeeded', rows_upserted = $2, finished_at = now()
WHERE id = $1;

-- name: FailSyncRun :exec
UPDATE sync_runs
SET status = 'failed', error = $2, rows_upserted = $3, finished_at = now()
WHERE id = $1;

-- name: LatestSyncRun :one
SELECT * FROM sync_runs
WHERE bulk_type = $1
ORDER BY started_at DESC
LIMIT 1;
