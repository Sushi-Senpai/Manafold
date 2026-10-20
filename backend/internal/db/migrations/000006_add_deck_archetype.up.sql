-- The deck's chosen archetype: a template key from internal/decktemplates, or
-- NULL when none is chosen. The handler validates the key against the template
-- set; there is deliberately no CHECK listing the keys, since the templates live
-- in code and a DB-side copy would need a migration per new template (DECK-103).
ALTER TABLE decks ADD COLUMN archetype TEXT;
