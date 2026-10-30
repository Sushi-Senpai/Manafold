-- The deck wizard's answers (setup restrictions, land and utility choices, land
-- target, rejected cards, current step) so a reload resumes the wizard
-- (WIZ-092). One row per deck, written only through the owner-gated state
-- endpoints; the server validates the JSON and never queries inside it.
CREATE TABLE deck_wizard_state (
    deck_id UUID PRIMARY KEY REFERENCES decks(id) ON DELETE CASCADE,
    state JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
