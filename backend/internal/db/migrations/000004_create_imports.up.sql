-- An import is an auditable two-step: POST /decks/{id}/import parses the raw
-- text and stores this row (raw text + structured parse + the names that did
-- not resolve); POST /decks/{id}/import/{importId}/apply then writes deck_cards
-- in one transaction (DECK-060). Storing all three of raw_text / parsed /
-- unresolved makes a bad parse diagnosable after the fact rather than silently
-- lossy (PORT-001, PORT-004).
CREATE TABLE imports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deck_id UUID REFERENCES decks(id) ON DELETE SET NULL,
    source_format TEXT NOT NULL CHECK (source_format IN ('plaintext', 'mtga', 'moxfield', 'archidekt')),
    raw_text TEXT NOT NULL,
    parsed JSONB NOT NULL DEFAULT '{}',
    unresolved JSONB NOT NULL DEFAULT '[]',
    applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_imports_deck_id ON imports (deck_id);
