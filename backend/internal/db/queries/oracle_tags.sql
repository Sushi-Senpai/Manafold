-- Oracle-tag reads over the mirror (CARD-065). Every read ignores a disabled
-- tag (CARD-063); "tag or any descendant" is oracle_tag_reach, the walk over
-- enabled tags (CARD-062, CARD-063).

-- A card's directly applied, enabled oracle tags.
-- @spec CARD-065, CARD-063
-- name: ListOracleTagsForCard :many
SELECT t.slug, t.label, ct.weight
FROM card_oracle_tags ct
JOIN oracle_tags t ON t.id = ct.tag_id
WHERE ct.card_id = sqlc.arg(card_id)
  AND NOT t.disabled
ORDER BY t.slug;

-- Cards carrying the tag with this slug or any descendant reachable through
-- enabled tags, optionally within a colour identity (an empty identity array
-- means no filter), best (lowest edhrec_rank) first.
-- @spec CARD-065, CARD-062, CARD-063
-- name: ListCardsByOracleTag :many
SELECT c.* FROM cards c
WHERE EXISTS (
    SELECT 1 FROM card_oracle_tags ct
    WHERE ct.card_id = c.id
      AND ct.tag_id IN (SELECT oracle_tag_reach(sqlc.arg(slug)::text))
)
  AND (cardinality(sqlc.arg(identity)::text[]) = 0
       OR c.color_identity <@ sqlc.arg(identity)::text[])
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT sqlc.arg(lim);

-- The subset of card_ids carrying the tag with this slug or any descendant
-- reachable through enabled tags — role counting over a deck's cards.
-- @spec CARD-065, CARD-062, CARD-063
-- name: FilterCardIDsByOracleTag :many
SELECT DISTINCT ct.card_id
FROM card_oracle_tags ct
WHERE ct.tag_id IN (SELECT oracle_tag_reach(sqlc.arg(slug)::text))
  AND ct.card_id = ANY(sqlc.arg(card_ids)::uuid[]);
