-- Oracle-tag reads over the mirror (CARD-065). Every read ignores a disabled
-- tag (CARD-063), and "tag or any descendant" goes through oracle_tag_closure
-- (CARD-062). The disabled flag is toggled without a sync, so the closure still
-- holds pairs that pass through a disabled tag: a tag-or-descendant read drops
-- the (queried, applied) pair when a disabled tag x lies on it — closure(a, x)
-- and closure(x, d) — which covers a and d themselves through their self rows.

-- A card's directly applied, enabled oracle tags.
-- @spec CARD-065, CARD-063
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
-- @spec CARD-065, CARD-062, CARD-063
-- name: ListCardsByOracleTag :many
SELECT c.* FROM cards c
WHERE EXISTS (
    SELECT 1
    FROM oracle_tags a
    JOIN oracle_tag_closure cl ON cl.ancestor_id = a.id
    JOIN oracle_tags d ON d.id = cl.descendant_id
    JOIN card_oracle_tags ct ON ct.tag_id = d.id
    WHERE a.slug = lower(sqlc.arg(slug)::text)
      AND NOT EXISTS (
          SELECT 1
          FROM oracle_tag_closure up
          JOIN oracle_tags x ON x.id = up.descendant_id AND x.disabled
          JOIN oracle_tag_closure down ON down.ancestor_id = x.id
          WHERE up.ancestor_id = a.id AND down.descendant_id = d.id
      )
      AND ct.card_id = c.id
)
  AND (cardinality(sqlc.arg(identity)::text[]) = 0
       OR c.color_identity <@ sqlc.arg(identity)::text[])
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT sqlc.arg(lim);

-- The subset of card_ids carrying the tag with this slug or any of its
-- descendants — role counting over a deck's cards.
-- @spec CARD-065, CARD-062, CARD-063
-- name: FilterCardIDsByOracleTag :many
SELECT DISTINCT ct.card_id
FROM oracle_tags a
JOIN oracle_tag_closure cl ON cl.ancestor_id = a.id
JOIN oracle_tags d ON d.id = cl.descendant_id
JOIN card_oracle_tags ct ON ct.tag_id = d.id
WHERE a.slug = lower(sqlc.arg(slug)::text)
  AND NOT EXISTS (
      SELECT 1
      FROM oracle_tag_closure up
      JOIN oracle_tags x ON x.id = up.descendant_id AND x.disabled
      JOIN oracle_tag_closure down ON down.ancestor_id = x.id
      WHERE up.ancestor_id = a.id AND down.descendant_id = d.id
  )
  AND ct.card_id = ANY(sqlc.arg(card_ids)::uuid[]);
