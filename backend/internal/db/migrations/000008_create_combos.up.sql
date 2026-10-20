-- Commander Spellbook combo mirror (CARD-050). Filled by cmd/cardsync's combo
-- pass from Spellbook's daily variants.json export; read by deck-building's
-- bracket check (DECK-122). Keyed to Scryfall oracle ids with no foreign key to
-- cards: the join happens at query time, so the combo pass runs independently
-- of the Scryfall passes. The classifier's inputs are stored verbatim; the
-- classification itself lives in internal/bracket. See
-- docs/intent/card-data/card-data-design.md § Commander Spellbook Combo Mirror.
CREATE TABLE combos (
    id TEXT PRIMARY KEY,
    identity TEXT NOT NULL,
    mana_needed TEXT NOT NULL DEFAULT '',
    mana_value_needed INTEGER NOT NULL,
    has_notable_prerequisites BOOLEAN NOT NULL,
    -- [{"name": "...", "status": "S|C|H|HU|PU"}] — every produced feature.
    produces JSONB NOT NULL,
    bracket_tag TEXT NOT NULL,
    popularity INTEGER
);

CREATE TABLE combo_cards (
    combo_id TEXT NOT NULL REFERENCES combos(id) ON DELETE CASCADE,
    scryfall_oracle_id UUID NOT NULL,
    quantity INTEGER NOT NULL,
    -- Spellbook zone letters the card starts in: B H G E L C, concatenated.
    zone_locations TEXT NOT NULL,
    must_be_commander BOOLEAN NOT NULL,
    PRIMARY KEY (combo_id, scryfall_oracle_id)
);

CREATE INDEX idx_combo_cards_oracle_id ON combo_cards (scryfall_oracle_id);

-- Spellbook's per-card flags (CARD-053); only flagged cards are stored.
CREATE TABLE spellbook_card_flags (
    scryfall_oracle_id UUID PRIMARY KEY,
    card_name TEXT NOT NULL,
    mass_land_denial BOOLEAN NOT NULL,
    extra_turn BOOLEAN NOT NULL
);
