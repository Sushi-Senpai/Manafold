-- Scryfall oracle tags (Tagger's community functional tags: ramp, mana-rock,
-- fetchland, utility-land, tutor, …), mirrored from the daily oracle_tags bulk
-- export by cmd/cardsync (CARD-010, CARD-060). The tag id is Scryfall's stable
-- UUID; slugs are renameable, so slug is indexed but not unique. disabled is
-- Manafold-owned — the sync never writes it — and every tag read ignores a
-- disabled tag (CARD-063).
CREATE TABLE oracle_tags (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL,
    label TEXT NOT NULL,
    description TEXT,
    parent_ids UUID[] NOT NULL DEFAULT '{}',
    aliases TEXT[] NOT NULL DEFAULT '{}',
    disabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_oracle_tags_slug ON oracle_tags (slug);

CREATE TRIGGER oracle_tags_set_updated_at
    BEFORE UPDATE ON oracle_tags
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Direct taggings only: one row per (card, tag) the export lists. Inherited
-- tags come from oracle_tag_closure. weight is text (median / strong /
-- very_strong today) so a new Scryfall weight never fails a sync.
CREATE TABLE card_oracle_tags (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES oracle_tags(id) ON DELETE CASCADE,
    weight TEXT NOT NULL,
    annotation TEXT,
    PRIMARY KEY (card_id, tag_id)
);

CREATE INDEX idx_card_oracle_tags_tag_id ON card_oracle_tags (tag_id);

-- Transitive closure of the tag hierarchy, rebuilt with every tag sync: one
-- row per tag paired with itself and with every tag reachable from it through
-- child links (CARD-062). "Cards with tag T or any descendant" is
-- ancestor_id = T joined to card_oracle_tags on descendant_id.
CREATE TABLE oracle_tag_closure (
    ancestor_id UUID NOT NULL REFERENCES oracle_tags(id) ON DELETE CASCADE,
    descendant_id UUID NOT NULL REFERENCES oracle_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (ancestor_id, descendant_id)
);

CREATE INDEX idx_oracle_tag_closure_descendant ON oracle_tag_closure (descendant_id);
