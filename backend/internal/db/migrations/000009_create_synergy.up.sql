-- Per-commander synergy aggregates computed from public Archidekt decklists,
-- gathered on demand by a background worker (docs/intent/synergy/). Only aggregates live here: no
-- decklist, deck id, deck name, or owner is ever stored (SYN-020). A target —
-- one commander sample or one colour-identity baseline — is replaced whole in a
-- single transaction (SYN-022).

-- One row per sampled commander. color_identity is the canonical WUBRG-ordered
-- letter string ("C" for colourless) that keys its baseline.
CREATE TABLE synergy_commanders (
    commander_oracle_id UUID PRIMARY KEY REFERENCES cards(scryfall_oracle_id) ON DELETE CASCADE,
    commander_name      TEXT NOT NULL,
    color_identity      TEXT NOT NULL,
    deck_count          INTEGER NOT NULL,
    duplicates_skipped  INTEGER NOT NULL DEFAULT 0,
    fetched_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_synergy_commanders_identity ON synergy_commanders (color_identity);

-- Per-card inclusion under one commander. card_oracle_id has no FK: a card new
-- enough to predate the next Scryfall sync is kept and joined at read time.
-- baseline_rate / synergy are NULL while the identity has no baseline sample
-- (SYN-021).
CREATE TABLE synergy_commander_cards (
    commander_oracle_id UUID NOT NULL REFERENCES synergy_commanders(commander_oracle_id) ON DELETE CASCADE,
    card_oracle_id      UUID NOT NULL,
    deck_count          INTEGER NOT NULL,
    inclusion_rate      DOUBLE PRECISION NOT NULL,
    baseline_rate       DOUBLE PRECISION,
    synergy             DOUBLE PRECISION,
    PRIMARY KEY (commander_oracle_id, card_oracle_id)
);

CREATE INDEX idx_synergy_commander_cards_rank
    ON synergy_commander_cards (commander_oracle_id, synergy DESC NULLS LAST);

-- One row per colour-identity baseline: recent decks of the top-ranked other
-- commanders in that exact identity (SYN-011).
CREATE TABLE synergy_baselines (
    color_identity      TEXT PRIMARY KEY,
    baseline_commanders TEXT[] NOT NULL DEFAULT '{}',
    deck_count          INTEGER NOT NULL,
    duplicates_skipped  INTEGER NOT NULL DEFAULT 0,
    fetched_at          TIMESTAMPTZ NOT NULL
);

CREATE TABLE synergy_baseline_cards (
    color_identity TEXT NOT NULL REFERENCES synergy_baselines(color_identity) ON DELETE CASCADE,
    card_oracle_id UUID NOT NULL,
    deck_count     INTEGER NOT NULL,
    inclusion_rate DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (color_identity, card_oracle_id)
);

-- The gathering queue and its audit trail (SYN-030..036). A job gathers one
-- commander; the partial unique index makes "at most one queued or running job
-- per commander" a database guarantee (SYN-031).
CREATE TABLE synergy_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    commander_oracle_id UUID NOT NULL REFERENCES cards(scryfall_oracle_id) ON DELETE CASCADE,
    status              TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    requested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    requests_made       INTEGER NOT NULL DEFAULT 0,
    error               TEXT
);

CREATE UNIQUE INDEX idx_synergy_jobs_one_active
    ON synergy_jobs (commander_oracle_id) WHERE status IN ('queued', 'running');

CREATE INDEX idx_synergy_jobs_requested ON synergy_jobs (requested_at);
