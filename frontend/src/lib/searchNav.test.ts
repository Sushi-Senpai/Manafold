import { test } from "node:test";
import assert from "node:assert/strict";

import { moveCursor, deckCardQuantity } from "./searchNav.ts";
import type { DeckDetail, DeckEntry } from "./api.ts";

// @spec DECK-082
test("moveCursor steps down and up and clamps at the ends without wrapping", () => {
  assert.equal(moveCursor(-1, 5, "ArrowDown"), 0);
  assert.equal(moveCursor(0, 5, "ArrowDown"), 1);
  assert.equal(moveCursor(4, 5, "ArrowDown"), 4);
  assert.equal(moveCursor(-1, 5, "ArrowUp"), 4);
  assert.equal(moveCursor(3, 5, "ArrowUp"), 2);
  assert.equal(moveCursor(0, 5, "ArrowUp"), 0);
});

// @spec DECK-082
test("moveCursor honours Home/End and ignores unrelated keys and empty lists", () => {
  assert.equal(moveCursor(3, 9, "Home"), 0);
  assert.equal(moveCursor(3, 9, "End"), 8);
  assert.equal(moveCursor(3, 9, "a"), 3);
  assert.equal(moveCursor(3, 9, "Enter"), 3);
  assert.equal(moveCursor(2, 0, "ArrowDown"), -1);
});

function entry(cardId: string, quantity: number, board: string): DeckEntry {
  return {
    entry_id: `${board}:${cardId}`,
    card_id: cardId,
    name: cardId,
    mana_cost: null,
    mana_value: 0,
    type_line: "",
    color_identity: [],
    quantity,
    board,
    category: null,
    image_uris: null,
    prices: null,
    set_code: "",
    collector_number: "",
    color_identity_violation: false,
    offending_colors: [],
    singleton_violation: false,
  };
}

const detail = {
  boards: {
    command: [entry("cmdr", 1, "command")],
    main: [entry("forest", 12, "main"), entry("solring", 1, "main")],
    maybe: [entry("forest", 3, "maybe")],
    sideboard: [],
  },
} as unknown as DeckDetail;

// @spec DECK-083
test("deckCardQuantity sums a card across every board and returns 0 when absent", () => {
  assert.equal(deckCardQuantity(detail, "forest"), 15);
  assert.equal(deckCardQuantity(detail, "solring"), 1);
  assert.equal(deckCardQuantity(detail, "cmdr"), 1);
  assert.equal(deckCardQuantity(detail, "island"), 0);
});
