-- @spec PORT-001
-- name: CreateImport :one
INSERT INTO imports (deck_id, source_format, raw_text, parsed, unresolved)
SELECT d.id,
       sqlc.arg(source_format)::text,
       sqlc.arg(raw_text)::text,
       sqlc.arg(parsed),
       sqlc.arg(unresolved)
FROM decks d
WHERE d.id = sqlc.arg(deck_id)::uuid
  AND (d.user_id = sqlc.narg(user_id)::uuid OR d.anon_token = sqlc.narg(anon_token)::text)
RETURNING *;

-- Ownership is scoped through the deck: an import for a deck the caller does not
-- own returns no row, which the handler maps to 404 (DECK-009).
-- @spec PORT-006, DECK-009
-- name: GetImportForOwner :one
SELECT i.*
FROM imports i
JOIN decks d ON d.id = i.deck_id
WHERE i.id = sqlc.arg(import_id)::uuid
  AND (d.user_id = sqlc.narg(user_id)::uuid OR d.anon_token = sqlc.narg(anon_token)::text);

-- The guard is atomic: the UPDATE claims the row only while applied_at is still
-- null, so two concurrent applies serialize on the row lock and exactly one sees
-- a row affected. Zero rows affected means the import was already applied and the
-- handler returns 409 (PORT-006).
-- @spec PORT-006
-- name: MarkImportApplied :execrows
UPDATE imports SET applied_at = now() WHERE id = $1 AND applied_at IS NULL;
