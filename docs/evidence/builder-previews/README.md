# Builder-experience pass — E2E screenshot evidence

Captured against the real builder page (`/decks/[id]`) running the full local
stack: Next.js dev server → same-origin `/api` proxy → Go API (`DEV_AUTH=true`)
→ Postgres seeded from `backend/seed/cards.json` (`CARDSYNC_SEED_PATH`). Viewport
1440×900. The demo deck is Atraxa (WUBG) with a spread of real cards plus
11× Forest and the two mirror-imageless cards (`Malakir Rebirth // Malakir Mire`,
`Fabricate`).

The `chrome-devtools-axi` bridge was non-functional in this environment (every
tool call returned `Invalid arguments … Required at pageId`), so the page was
driven over the Chrome DevTools Protocol directly against a headless Chromium —
the same browser automation, minus the broken wrapper. Headless Chromium reports
no pointing device, so a `window.matchMedia` shim (the equivalent of
Playwright's `emulateMedia`) was injected via
`Page.addScriptToEvaluateOnNewDocument` to report a fine hover pointer; the
application code under test is unchanged. Driver script: not committed (throwaway
CDP harness).

| File | Shows | Specs |
|---|---|---|
| `01-hover-preview-search-result.png` | Hovering a search result ("Counterspell") floats its real card image beside the row | DECK-070, DECK-071, DECK-076 |
| `02-hover-preview-decklist-row.png` | Hovering a decklist row ("Sol Ring") floats its card image over the decklist; action menu opening alongside | DECK-070, DECK-076, DECK-090 |
| `03-enriched-search-results.png` | `t:creature` results with name, mana pips, type line, colour-identity dots, "N in deck" badges, one-click Add; decklist quantity steppers (`− 11 +` on Forest) | DECK-080, DECK-083, DECK-085, DECK-086 |
| `04-decklist-action-menu.png` | The hover action menu — Add One `Alt+1`, Add More…, Remove `Alt+2`, Move to Sideboard `Alt+3`, Move to Considering `Alt+4` — anchored to a decklist row, not overlapping the preview | DECK-090, DECK-091, DECK-092 |
| `05-preview-text-frame-fallback.png` | Hovering an imageless card ("Malakir Rebirth // Malakir Mire") shows a text card frame ("NO CARD IMAGE"), never a broken image | DECK-071 |

## After the M4 (AI assist) rebase

This branch was rebased onto the merged M4 milestone. `06`–`08` show M4's
AI-assist controls and this pass's componentised builder working together; the
`01`–`05` surfaces are unchanged by that integration.

| File | Shows | Specs |
|---|---|---|
| `06-componentised-builder-with-explain-fit.png` | The full builder — componentised commander picker / card search / decklist (`× 1 +` steppers) — with M4's per-row "Explain fit" control under every main / command row | DECK-004, DECK-007, DECK-086, AI-021 |
| `07-decklist-hover-preview-action-menu-explain-fit.png` | Hovering the "Counterspell" decklist row at once: the floating card-image preview, the hover action menu (Add One `Alt+1` … Copy Card Name), and the "Explain fit" footer on that row and every other | DECK-070, DECK-076, DECK-090, DECK-092, AI-021 |
| `08-ai-suggestions-panel.png` | M4's "AI suggestions" panel mounted alongside the other builder panels (collapsed until "Suggest cards" is pressed); suggested card names raise the same shared hover preview | AI-020 |
