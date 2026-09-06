// Pure helpers for the builder's card-search result list: keyboard cursor
// movement and the "already in this deck" quantity lookup. Framework-free so the
// navigation and lookup logic is unit-testable without a DOM (searchNav.test.ts).
//
// @spec DECK-082, DECK-083

import type { DeckDetail } from "./api";

// moveCursor returns the next cursor index for a key pressed over a list of
// `count` rows. ArrowDown / ArrowUp step and clamp at the ends (no wrap); Home /
// End jump to the first / last row; any other key leaves the cursor unchanged.
// A cursor of -1 means "nothing focused yet" — the first ArrowDown lands on row
// 0 and the first ArrowUp lands on the last row. An empty list stays at -1.
//
// @spec DECK-082
export function moveCursor(current: number, count: number, key: string): number {
  if (count <= 0) return -1;
  const clamp = (n: number) => Math.min(Math.max(n, 0), count - 1);
  switch (key) {
    case "ArrowDown":
      return current < 0 ? 0 : clamp(current + 1);
    case "ArrowUp":
      return current < 0 ? count - 1 : clamp(current - 1);
    case "Home":
      return 0;
    case "End":
      return count - 1;
    default:
      return current;
  }
}

// deckCardQuantity sums a card's quantity across every board of a deck, so a
// search result can show "3 in deck" instead of silently stacking another copy.
// Returns 0 when the card is not in the deck.
//
// @spec DECK-083
export function deckCardQuantity(detail: Pick<DeckDetail, "boards">, cardId: string): number {
  let total = 0;
  for (const entries of Object.values(detail.boards)) {
    for (const entry of entries) {
      if (entry.card_id === cardId) total += entry.quantity;
    }
  }
  return total;
}
