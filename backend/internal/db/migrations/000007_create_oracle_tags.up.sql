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
-- tags come from oracle_tag_reach (below). weight is text (median / strong /
-- very_strong today) so a new Scryfall weight never fails a sync.
CREATE TABLE card_oracle_tags (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES oracle_tags(id) ON DELETE CASCADE,
    weight TEXT NOT NULL,
    annotation TEXT,
    PRIMARY KEY (card_id, tag_id)
);

CREATE INDEX idx_card_oracle_tags_tag_id ON card_oracle_tags (tag_id);

-- The enabled tags reachable from the tag(s) with this slug: the tag itself and
-- every descendant joined to it by a path of enabled tags through child links
-- (the inverse of parent_ids). The walk never enters a disabled tag, so a
-- disabled queried tag reaches nothing and a descendant reachable only through
-- a disabled tag drops out, while one still reachable through an enabled path
-- stays (CARD-062, CARD-063). It reads disabled live, so a maintainer's toggle
-- takes effect without a sync; UNION makes a cycle reach a fixed point. Every
-- tag-or-descendant read — the sqlc tag queries and otag: search — goes through
-- this one definition.
CREATE FUNCTION oracle_tag_reach(root_slug TEXT) RETURNS SETOF UUID
LANGUAGE sql STABLE AS $$
    WITH RECURSIVE walk (id) AS (
        SELECT t.id FROM oracle_tags t
        WHERE t.slug = lower(root_slug) AND NOT t.disabled
        UNION
        SELECT c.id
        FROM walk w
        JOIN oracle_tags c ON w.id = ANY (c.parent_ids)
        WHERE NOT c.disabled
    )
    SELECT id FROM walk
$$;
