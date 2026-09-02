---
parent: high-level-design
prefix: COLL
---

# Collections (deferred)

Collection tracking — recording which cards a user physically owns and letting
the builder mark deck cards as owned / needed, plus a "build with owned only"
filter and CSV import of a collection export — is deferred past v1 (roadmap M8).
It has no v1 schema, endpoints, or UI, and no EARS specs yet. The `card-data`
mirror is the join target a future `owned_cards` table (`user_id`, `card_id`,
`quantity`, `finish`, `condition`) would reference, and `deck-building`'s
`DeckDetail` response is where an "owned" flag per entry would surface; nothing
in the current design precludes adding either. This stub exists so the segment
has a home when it is picked up.
