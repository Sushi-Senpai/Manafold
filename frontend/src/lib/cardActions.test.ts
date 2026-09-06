import { test } from "node:test";
import assert from "node:assert/strict";

import {
  buildCardActions,
  findShortcutAction,
  type CardActionEntry,
  type DeckCardApi,
} from "./cardActions.ts";

const noopApi: DeckCardApi = {
  addCard: async () => {},
  removeCard: async () => {},
  setCardQuantity: async () => {},
  moveCard: async () => {},
};

const base = { deckId: "d1", api: noopApi, onChange: () => {} };

function ids(entry: CardActionEntry) {
  return buildCardActions({ ...base, entry }).map((a) => a.id);
}

// @spec DECK-091
test("a single-copy main entry offers add/remove and moves to the other boards only", () => {
  assert.deepEqual(ids({ card_id: "c", name: "Sol Ring", board: "main", quantity: 1 }), [
    "add-one",
    "add-more",
    "remove-one",
    "move-sideboard",
    "move-maybe",
    "copy-name",
  ]);
});

// @spec DECK-091
test("a multi-copy entry also offers Remove All", () => {
  const list = ids({ card_id: "c", name: "Forest", board: "main", quantity: 9 });
  assert.ok(list.includes("remove-all"));
  assert.ok(!list.includes("move-main"));
});

// @spec DECK-091
test("a sideboard entry can move to main or considering but not to sideboard", () => {
  const list = ids({ card_id: "c", name: "Card", board: "sideboard", quantity: 1 });
  assert.ok(list.includes("move-main"));
  assert.ok(list.includes("move-maybe"));
  assert.ok(!list.includes("move-sideboard"));
});

// @spec DECK-093
test("the commander entry offers only Copy Card Name — no quantity or board edits", () => {
  assert.deepEqual(ids({ card_id: "c", name: "Atraxa", board: "command", quantity: 1 }), [
    "copy-name",
  ]);
});

// @spec DECK-092
test("findShortcutAction maps Alt+digit chords to the right actions", () => {
  const actions = buildCardActions({
    ...base,
    entry: { card_id: "c", name: "Forest", board: "main", quantity: 3 },
  });
  assert.equal(findShortcutAction(actions, "Alt+1")?.id, "add-one");
  assert.equal(findShortcutAction(actions, "Alt+2")?.id, "remove-one");
  assert.equal(findShortcutAction(actions, "Alt+3")?.id, "move-sideboard");
  assert.equal(findShortcutAction(actions, "Alt+4")?.id, "move-maybe");
  assert.equal(findShortcutAction(actions, "Alt+9"), undefined);
});
