-- Shared updated_at trigger, defined once here (the first migration) and
-- attached BEFORE UPDATE to every table below that carries an updated_at.
CREATE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Oracle layer: one row per Scryfall oracle_id. The unit legality is checked
-- against. color_identity is stored exactly as Scryfall provides it and is
-- never recomputed (CARD-002). singleton_limit / can_be_commander /
-- commander_color_identity are derived at sync time from Oracle text
-- (CARD-003, CARD-004).
CREATE TABLE cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scryfall_oracle_id UUID NOT NULL UNIQUE,
    name TEXT NOT NULL,
    mana_cost TEXT,
    mana_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    type_line TEXT NOT NULL DEFAULT '',
    oracle_text TEXT NOT NULL DEFAULT '',
    colors TEXT[] NOT NULL DEFAULT '{}',
    color_identity TEXT[] NOT NULL DEFAULT '{}',
    produced_mana TEXT[] NOT NULL DEFAULT '{}',
    keywords TEXT[] NOT NULL DEFAULT '{}',
    power TEXT,
    toughness TEXT,
    loyalty TEXT,
    legalities JSONB NOT NULL DEFAULT '{}',
    is_game_changer BOOLEAN NOT NULL DEFAULT false,
    is_reserved BOOLEAN NOT NULL DEFAULT false,
    layout TEXT NOT NULL DEFAULT 'normal',
    card_faces JSONB,
    singleton_limit INTEGER,
    can_be_commander BOOLEAN NOT NULL DEFAULT false,
    commander_color_identity TEXT[],
    edhrec_rank INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_cards_lower_name ON cards (lower(name));
CREATE INDEX idx_cards_name_prefix ON cards (lower(name) text_pattern_ops);
CREATE INDEX idx_cards_mana_value ON cards (mana_value);
CREATE INDEX idx_cards_edhrec_rank ON cards (edhrec_rank);
CREATE INDEX idx_cards_color_identity ON cards USING GIN (color_identity);
-- Full-text search over name + oracle text + type line. The search handler
-- (internal/cardsearch) builds the same to_tsvector expression in its WHERE
-- clause so this expression index is used. All three columns are NOT NULL so
-- the concatenation is never NULL.
CREATE INDEX idx_cards_oracle_fts ON cards
    USING GIN (to_tsvector('english', name || ' ' || oracle_text || ' ' || type_line));

CREATE TRIGGER cards_set_updated_at
    BEFORE UPDATE ON cards
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Printing layer: one row per Scryfall printing id. The unit display uses.
CREATE TABLE card_prints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scryfall_id UUID NOT NULL UNIQUE,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    set_code TEXT NOT NULL,
    set_name TEXT NOT NULL DEFAULT '',
    collector_number TEXT NOT NULL DEFAULT '',
    rarity TEXT NOT NULL DEFAULT '',
    released_at DATE,
    finishes TEXT[] NOT NULL DEFAULT '{}',
    image_uris JSONB,
    prices JSONB,
    is_promo BOOLEAN NOT NULL DEFAULT false,
    is_reprint BOOLEAN NOT NULL DEFAULT false,
    is_digital BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_card_prints_card_id_newest ON card_prints (card_id, released_at DESC NULLS LAST);

CREATE TRIGGER card_prints_set_updated_at
    BEFORE UPDATE ON card_prints
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Rulings are re-synced by deleting a card's existing rows and re-inserting,
-- so no in-table dedup constraint is needed (rulings ingestion is optional and
-- deferred past M1 — see docs/intent/card-data/, CARD-010's sibling note).
CREATE TABLE card_rulings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    scryfall_oracle_id UUID NOT NULL,
    comment TEXT NOT NULL,
    published_at DATE
);

CREATE INDEX idx_card_rulings_card_id ON card_rulings (card_id);

-- Bulk-data ingestion audit. The sync is load-bearing, so it gets its own
-- table and its own test (CARD-001, CARD-007).
CREATE TABLE sync_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bulk_type TEXT NOT NULL,
    scryfall_updated_at TIMESTAMPTZ,
    rows_upserted INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'running',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    error TEXT
);

CREATE INDEX idx_sync_runs_bulk_type_started ON sync_runs (bulk_type, started_at DESC);

-- Escape hatch for the gap between a Commander Format Panel announcement and
-- Scryfall's next data refresh. deck-building's validator consults this after
-- legalities->>'commander' (CARD-030, DECK-008). banned = false un-bans a card
-- Scryfall still lists as banned.
CREATE TABLE banlist_overrides (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_name TEXT NOT NULL UNIQUE,
    banned BOOLEAN NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
