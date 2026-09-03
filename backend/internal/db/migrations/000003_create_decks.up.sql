-- A deck is owned by exactly one of a real user or an anonymous-draft token
-- (the CHECK enforces exactly-one). color_identity is denormalised: the union
-- of the commander(s)' commander colour identities, recomputed by the handler
-- on every commander change (DECK-003).
CREATE TABLE decks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    anon_token TEXT,
    name TEXT NOT NULL DEFAULT 'Untitled deck',
    description TEXT NOT NULL DEFAULT '',
    commander_card_id UUID REFERENCES cards(id),
    partner_card_id UUID REFERENCES cards(id),
    format TEXT NOT NULL DEFAULT 'commander',
    bracket INTEGER,
    power_estimate DOUBLE PRECISION,
    is_public BOOLEAN NOT NULL DEFAULT false,
    color_identity TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT decks_one_owner CHECK ((user_id IS NULL) <> (anon_token IS NULL))
);

CREATE INDEX idx_decks_user_id ON decks (user_id);
CREATE INDEX idx_decks_anon_token ON decks (anon_token);

CREATE TRIGGER decks_set_updated_at
    BEFORE UPDATE ON decks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One entry per card per board. quantity > 1 is only legal for basic lands
-- and singleton_limit cards; the validator (internal/deckrules) enforces that,
-- not a DB constraint. category is free-text in M1 (the functional
-- auto-categorizer is later).
CREATE TABLE deck_cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deck_id UUID NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES cards(id),
    print_id UUID REFERENCES card_prints(id),
    quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity > 0),
    board TEXT NOT NULL CHECK (board IN ('command', 'main', 'maybe', 'sideboard')),
    category TEXT,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (deck_id, card_id, board)
);

CREATE INDEX idx_deck_cards_deck_id ON deck_cards (deck_id);
