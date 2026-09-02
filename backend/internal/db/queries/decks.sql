-- @spec DECK-001
-- name: CreateDeck :one
INSERT INTO decks (user_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: ListDecksByUser :many
SELECT * FROM decks WHERE user_id = $1 ORDER BY updated_at DESC;

-- Ownership is scoped in the query: a deck the caller does not own returns no
-- row, which the handler maps to 404 (DECK-009).
-- @spec DECK-009
-- name: GetDeckForUser :one
SELECT * FROM decks WHERE id = $1 AND user_id = $2;

-- @spec DECK-030, DECK-031
-- name: GetPublicDeck :one
SELECT * FROM decks WHERE id = $1 AND is_public = true;

-- @spec DECK-002, DECK-003
-- name: SetDeckCommander :one
UPDATE decks
SET commander_card_id = sqlc.narg(commander_card_id),
    partner_card_id = sqlc.narg(partner_card_id),
    color_identity = sqlc.arg(color_identity)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- @spec DECK-011
-- name: UpdateDeckMeta :one
UPDATE decks
SET name = sqlc.arg(name),
    description = sqlc.arg(description),
    is_public = sqlc.arg(is_public),
    bracket = sqlc.narg(bracket)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- Ownership is scoped in the query, exactly like the other deck_cards
-- mutations: the INSERT ... SELECT draws deck_id from a decks row filtered by
-- owner, so adding a card to a deck the caller does not own produces no row and
-- the handler maps "no rows" to 404 (DECK-009).
-- @spec DECK-005, DECK-009
-- name: AddDeckCard :one
INSERT INTO deck_cards (deck_id, card_id, print_id, quantity, board, category)
SELECT d.id,
       sqlc.arg(card_id)::uuid,
       sqlc.narg(print_id)::uuid,
       sqlc.arg(quantity)::integer,
       sqlc.arg(board)::text,
       sqlc.narg(category)::text
FROM decks d
WHERE d.id = sqlc.arg(deck_id)::uuid
  AND d.user_id = sqlc.arg(user_id)::uuid
ON CONFLICT (deck_id, card_id, board)
DO UPDATE SET quantity = deck_cards.quantity + EXCLUDED.quantity,
              category = COALESCE(EXCLUDED.category, deck_cards.category)
RETURNING *;

-- The DELETE is joined to decks filtered by owner, so removing a card from a
-- deck the caller does not own matches no row; the handler maps zero rows
-- affected to 404 (DECK-009).
-- @spec DECK-009, DECK-010
-- name: DeleteDeckCard :execrows
DELETE FROM deck_cards dc
USING decks d
WHERE dc.deck_id = sqlc.arg(deck_id)
  AND dc.card_id = sqlc.arg(card_id)
  AND dc.board = sqlc.arg(board)
  AND dc.deck_id = d.id
  AND d.user_id = sqlc.arg(user_id);

-- @spec DECK-007
-- name: ListDeckCardEntries :many
SELECT
    dc.id AS entry_id,
    dc.card_id,
    dc.print_id,
    dc.quantity,
    dc.board,
    dc.category,
    c.name,
    c.mana_cost,
    c.mana_value,
    c.type_line,
    c.oracle_text,
    c.color_identity,
    c.keywords,
    c.singleton_limit,
    c.can_be_commander,
    COALESCE(c.legalities ->> 'commander', '')::text AS legalities_commander,
    newest.image_uris,
    newest.prices,
    COALESCE(newest.set_code, '')::text AS set_code,
    COALESCE(newest.collector_number, '')::text AS collector_number
FROM deck_cards dc
JOIN cards c ON c.id = dc.card_id
LEFT JOIN LATERAL (
    SELECT image_uris, prices, set_code, collector_number
    FROM card_prints
    WHERE card_id = dc.card_id
    ORDER BY released_at DESC NULLS LAST
    LIMIT 1
) newest ON true
WHERE dc.deck_id = $1
ORDER BY
    CASE dc.board WHEN 'command' THEN 0 WHEN 'main' THEN 1 WHEN 'maybe' THEN 2 ELSE 3 END,
    dc.category NULLS LAST,
    c.name;
