# builder-ux-v2 — test-phase evidence

Independent end-to-end verification of the four captain corrections on the
`/decks/[id]` deck builder (branch `fm/manafold-builder-ux-v2`, target
`360131cf`).

## How these were produced

The **real, unmodified frontend** was built and served with `next dev`
(Next.js 16.3.1, Turbopack) from `frontend/`. Because this environment has no
Docker / Postgres, the Go backend was replaced by a small stub HTTP server
(`_mock-backend.mjs`) that serves the exact `/api/*` contract the builder calls
(`GET /api/decks/:id`, `/validation`, `/stats`, `/api/cards/search`, and a
mutating `POST /api/decks/:id/cards`). `next.config.ts`'s `BACKEND_ORIGIN` was
pointed at it via env var only — no source was changed. The page was driven
through the Chrome DevTools Protocol with real input events
(`_driver-cdp.mjs`); card art is fetched live from the Scryfall CDN.

Demo deck: "Kenrith Five-Colour Goodstuff" — Kenrith commander plus a spread
across every card type, including a planeswalker (*Chandra, Torch of Defiance*)
and a transform battle DFC (*Invasion of Zendikar // Awakened Skyclave*).

## What each shot shows

| File | Correction | Shows |
|---|---|---|
| `01-three-column-layout.png` | 4 | Three-column workspace: sticky left column (card-image panel + collapsible **Deck stats**), centre decklist, right column with **AI Suggestions** above **Add cards**. Slim legality summary pinned under the header, outside the columns. **Tools** menu on the header. |
| `02-decklist-type-grouped.png` | 4 | Full decklist. Board split is the outer level (`COMMANDER · 1`, `MAINBOARD · 46`, `CONSIDERING · 1`, `SIDEBOARD · 1`); commander is its own section; within a board, groups in display order — Creatures, Instants, Sorceries, Artifacts, Enchantments, Planeswalkers, Battles, Lands — each header with a copy count. The battle DFC lands under **Battles** (front face wins). |
| `03-image-panel-on-hover.png` | 3 | Hovering the "Cyclonic Rift" decklist row updates the sticky left-column image panel to that card's real art. No floating tooltip anywhere. |
| `07-image-panel-commander-at-rest.png` | 3 | Pointer moved away → the panel debounces back to the deck's commander, tagged `COMMANDER`. |
| `04a-search-before-click.png` | 2 | A "Beast" search: each result row has name, mana pips, type line, colour-identity dot, a "3 in deck" indicator, and only a small secondary `⋯` — no primary Add button. |
| `04b-click-to-add-confirmation.png` | 2 | One click on the "Beast Within" result row (not the `⋯`) added it to main: "Added ✓" flashes, "3 in deck" → "4 in deck", legality `47/100` → `48/100`, `MAINBOARD · 46` → `47`, `Instants (6)` → `(7)`, the decklist row goes `2` → `3`. A single click adds exactly one copy. |
| `04c-after-add-count.png` | 2 | The settled state after the confirmation clears — count stays at 48/47. |
| `05a-action-menu-open-on-click.png` | 1 | Clicking the "Solemn Simulacrum" row's `⋯` (no hover) opens the action menu **below** the row, right-aligned, not covering it: Add One `Alt+1`, Add More…, Remove `Alt+2`, Move to Sideboard `Alt+3`, Move to Considering `Alt+4`, Copy Card Name. |
| `05b-action-menu-closed-after-scroll.png` | 1 | After scrolling the list the menu is gone (closes on scroll); the left column and legality summary stay pinned. |
| `06-responsive-single-column.png` | 4 | At 760 px wide the workspace collapses to one column via flex order: pinned legality summary, commander picker, AI suggestions (moved up), search, then the type-grouped decklist. |

## Also run

- `frontend`: `yarn test` → 31/31 `node --test` pure-helper tests pass
  (`menuPlacement.test.ts`, `deck.test.ts` groupByType / primaryCardType /
  activeShortcutRow / resolvePanelCard, `cardPreview.test.ts`,
  `cardActions.test.ts`).
- `frontend`: `yarn build` (`next build`) → compiles clean, TypeScript passes,
  `/decks/[id]` route builds.
