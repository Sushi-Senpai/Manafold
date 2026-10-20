-- Commander Spellbook combo mirror (card-data, CARD-050..054) and the reads the
-- deck bracket check makes over it (deck-building, DECK-122, DECK-124, DECK-126).

-- @spec CARD-050
-- name: FinishSyncRunWithSource :exec
UPDATE sync_runs
SET status = 'succeeded', rows_upserted = $2, scryfall_updated_at = $3, finished_at = now()
WHERE id = $1;

-- @spec CARD-054
-- name: LatestSucceededSyncRun :one
SELECT * FROM sync_runs
WHERE bulk_type = $1 AND status = 'succeeded'
ORDER BY started_at DESC
LIMIT 1;

-- @spec CARD-053
-- name: DeleteAllSpellbookCardFlags :exec
DELETE FROM spellbook_card_flags;

-- @spec CARD-053
-- name: InsertSpellbookCardFlag :exec
INSERT INTO spellbook_card_flags (scryfall_oracle_id, card_name, mass_land_denial, extra_turn)
VALUES ($1, $2, $3, $4)
ON CONFLICT (scryfall_oracle_id) DO UPDATE SET
    card_name = EXCLUDED.card_name,
    mass_land_denial = spellbook_card_flags.mass_land_denial OR EXCLUDED.mass_land_denial,
    extra_turn = spellbook_card_flags.extra_turn OR EXCLUDED.extra_turn;

-- @spec DECK-126
-- name: ComboDataAvailable :one
SELECT EXISTS (SELECT 1 FROM combos) AS available;

-- Combos every one of whose card ingredients is among the given oracle ids —
-- the deck's main + command cards. Candidates are found through the
-- scryfall_oracle_id index, then any combo needing a card outside the set is
-- discarded. Quantities are re-checked by internal/bracket (DECK-122).
--
-- @spec DECK-122
-- name: ListCombosWithin :many
SELECT c.id, c.mana_needed, c.mana_value_needed, c.has_notable_prerequisites, c.produces, c.bracket_tag
FROM combos c
WHERE c.id IN (
    SELECT DISTINCT cc.combo_id FROM combo_cards cc
    WHERE cc.scryfall_oracle_id = ANY(sqlc.arg(oracle_ids)::uuid[])
)
AND NOT EXISTS (
    SELECT 1 FROM combo_cards miss
    WHERE miss.combo_id = c.id
      AND NOT (miss.scryfall_oracle_id = ANY(sqlc.arg(oracle_ids)::uuid[]))
)
ORDER BY c.id;

-- @spec DECK-122
-- name: ListComboCards :many
SELECT combo_id, scryfall_oracle_id, quantity, zone_locations, must_be_commander
FROM combo_cards
WHERE combo_id = ANY(sqlc.arg(combo_ids)::text[])
ORDER BY combo_id, scryfall_oracle_id;

-- @spec DECK-124
-- name: ListSpellbookCardFlags :many
SELECT scryfall_oracle_id, card_name, mass_land_denial, extra_turn
FROM spellbook_card_flags
WHERE scryfall_oracle_id = ANY(sqlc.arg(oracle_ids)::uuid[]);
