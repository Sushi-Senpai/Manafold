---
parent: high-level-design
prefix: SOCIAL
---

# Social (deferred)

Social features — a public deck feed, comments, likes, following, and primers as
a first-class artefact — are deferred past v1. v1 ships exactly one piece of
sharing: the read-only public deck URL, which is owned by `deck-building`
(`DECK-030` / `DECK-031`), not this segment. A future feed would read the same
`decks` rows where `is_public = true`; comments and follows would be new tables
(`deck_comments`, `user_follows`) with no dependency that the current schema
blocks. `decks.description` already holds a plain-markdown primer, so promoting
primers to a richer artefact is additive. This stub exists so the segment has a
home when it is picked up (roadmap: post-v1).
