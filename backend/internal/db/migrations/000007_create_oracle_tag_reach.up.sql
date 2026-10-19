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
