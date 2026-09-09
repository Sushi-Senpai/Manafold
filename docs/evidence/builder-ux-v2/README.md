# Builder UX v2 — E2E screenshot evidence

Captured against the real builder page (`/decks/[id]`) running the full local
stack: Next.js dev server → same-origin `/api` proxy → Go API (`DEV_AUTH=true`) →
Postgres (card mirror seeded from `backend/seed/cards.json` via
`CARDSYNC_SEED_PATH`, on top of a fuller prior sync). Desktop viewport 1560×1000;
`06` is 760×1100. The demo deck is "Kenrith Five-Colour Goodstuff" — Kenrith,
the Returned King (WUBRG) with a spread of real cards across every card type
(including the seed's new planeswalker, *Chandra, Torch of Defiance*, and battle,
*Invasion of Zendikar // Awakened Skyclave*) plus a 21-card basic-land base.

The `chrome-devtools-axi` wrapper was non-functional in this environment (every
page-scoped MCP call returned `Invalid arguments … Required at pageId` against
the bundled `chrome-devtools-mcp@1.9.0`), so the page was driven with
`puppeteer-core` against a headless Chromium — the same browser automation, minus
the broken wrapper. The application code under test is unchanged. Driver script:
throwaway, not committed.

| File | Shows | Specs |
|---|---|---|
| `01-three-column-layout.png` | The full builder as a three-column workspace — sticky left column (card-image panel + collapsible deck stats), centre decklist, right column with AI suggestions on top and card search below; slim always-visible legality summary under the header; import/export behind the header **Tools** menu | DECK-096, DECK-095, DECK-070 |
| `01b-three-column-viewport.png` | The same layout at one viewport height | DECK-096 |
| `02-decklist-type-grouped.png` | The decklist grouped by primary card type under the board split — `Commander · 1`, then `Mainboard` with `Creatures (4)`, `Instants (5)`, `Sorceries (4)`, `Artifacts (4)`, `Enchantments (3)`, `Planeswalkers (1)`, `Battles (1)`, `Lands (23)` — each header carrying its copy count | DECK-087, DECK-088 |
| `03-image-panel-on-hover.png` | Hovering the "Cyclonic Rift" decklist row updates the sticky left-column image panel to that card's art (no floating tooltip anywhere) | DECK-070, DECK-072, DECK-076, DECK-078 |
| `07-image-panel-commander-at-rest.png` | With nothing hovered, the image panel shows the deck's commander, tagged `COMMANDER` | DECK-072 |
| `04a-search-before-click.png` | A search result row for "Beast Within" — name, mana pips, type line, colour identity, "2 in deck" indicator, and a small secondary `⋯` for other boards; no separate primary "Add" button | DECK-080, DECK-081, DECK-083 |
| `04b-click-to-add-confirmation.png` | A click on the result row added the card to `main` — "Added ✓" confirmation on the row, decklist count `27 → 28`, "Beast Within" now under `Instants (5)` | DECK-081, DECK-084 |
| `05a-action-menu-open-on-click.png` | The action menu opened from the "Solemn Simulacrum" row's `⋯` (never on hover) — Add One `Alt+1`, Add More…, Remove `Alt+2`, Move to Sideboard `Alt+3`, Move to Considering `Alt+4`, Copy Card Name — positioned below the row, not covering it | DECK-090, DECK-091, DECK-094 |
| `05b-action-menu-closed-after-scroll.png` | After scrolling the page the menu is gone — it closes on scroll (as well as outside-click / Escape / navigation); the left column and legality summary stay pinned | DECK-094 |
| `06-responsive-single-column.png` | Narrow viewport (760px): the workspace collapses to one column — commander picker, AI suggestions, search, the type-grouped decklist (all boards), then the image panel and stats — with the legality summary still pinned | DECK-096 |

The `Alt+1..4` shortcut binding (bound to the row under the pointer or with
keyboard focus-within, independent of the menu being open or rendered) is a
non-visual behaviour covered by `frontend/src/lib/cardActions.test.ts`
(`findShortcutAction`) and exercised through `DecklistRow`'s effect.
