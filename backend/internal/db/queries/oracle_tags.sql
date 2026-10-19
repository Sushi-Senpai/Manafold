-- Oracle-tag reads over the mirror (CARD-026). Every read ignores a disabled
-- tag (CARD-019), and "tag or any descendant" goes through oracle_tag_closure
-- (CARD-018).

-- A card's directly applied, enabled oracle tags.
-- @spec CARD-026, CARD-019
-- name: ListOracleTagsForCard :many
SELECT t.slug, t.label, ct.weight
FROM card_oracle_tags ct
JOIN oracle_tags t ON t.id = ct.tag_id
WHERE ct.card_id = sqlc.arg(card_id)
  AND NOT t.disabled
ORDER BY t.slug;

-- Cards carrying the tag with this slug or any of its descendants, optionally
-- within a colour identity (an empty identity array means no filter), best
-- (lowest edhrec_rank) first.
-- @spec CARD-026, CARD-018, CARD-019
-- name: ListCardsByOracleTag :many
SELECT c.* FROM cards c
WHERE EXISTS (
    SELECT 1
    FROM oracle_tags a
    JOIN oracle_tag_closure cl ON cl.ancestor_id = a.id
    JOIN oracle_tags d ON d.id = cl.descendant_id
    JOIN card_oracle_tags ct ON ct.tag_id = d.id
    WHERE a.slug = lower(sqlc.arg(slug)::text)
      AND NOT a.disabled
      AND NOT d.disabled
      AND ct.card_id = c.id
)
  AND (cardinality(sqlc.arg(identity)::text[]) = 0
       OR c.color_identity <@ sqlc.arg(identity)::text[])
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT sqlc.arg(lim);

-- The subset of card_ids carrying the tag with this slug or any of its
-- descendants — role counting over a deck's cards.
-- @spec CARD-026, CARD-018, CARD-019
-- name: FilterCardIDsByOracleTag :many
SELECT DISTINCT ct.card_id
FROM oracle_tags a
JOIN oracle_tag_closure cl ON cl.ancestor_id = a.id
JOIN oracle_tags d ON d.id = cl.descendant_id
JOIN card_oracle_tags ct ON ct.tag_id = d.id
WHERE a.slug = lower(sqlc.arg(slug)::text)
  AND NOT a.disabled
  AND NOT d.disabled
  AND ct.card_id = ANY(sqlc.arg(card_ids)::uuid[]);
