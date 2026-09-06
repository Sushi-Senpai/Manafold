// Builds the ordered list of actions the builder's card action menu offers for a
// decklist entry, filtered to what is valid for that entry in its context: no
// board moves onto the board it is already on, and nothing that edits quantity
// or board for the commander. Framework-free (the deck API is passed in) so the
// filtering is unit-tested (cardActions.test.ts); the menu component renders the
// list and wires keyboard.
//
// @spec DECK-090, DECK-091, DECK-093

import type { DeckEntry } from "./api";

export type CardAction = {
  id: string;
  label: string;
  // Display form of the hover shortcut, e.g. "Alt+1"; also the key the menu's
  // shortcut handler matches on.
  shortcut?: string;
  danger?: boolean;
  run: () => Promise<unknown> | void;
};

export type CardActionEntry = Pick<DeckEntry, "card_id" | "name" | "board" | "quantity">;

// The slice of the deck API the actions call — passed in rather than imported so
// this module stays a pure, DOM-free unit.
export type DeckCardApi = {
  addCard: (deckId: string, cardId: string, board?: string) => Promise<unknown>;
  removeCard: (deckId: string, cardId: string, board?: string) => Promise<unknown>;
  setCardQuantity: (deckId: string, cardId: string, board: string, quantity: number) => Promise<unknown>;
  moveCard: (deckId: string, cardId: string, board: string, toBoard: string) => Promise<unknown>;
};

const MOVE_TARGETS: { board: string; label: string; shortcut?: string }[] = [
  { board: "sideboard", label: "Move to Sideboard", shortcut: "Alt+3" },
  { board: "maybe", label: "Move to Considering", shortcut: "Alt+4" },
  { board: "main", label: "Move to Main" },
];

export function buildCardActions(opts: {
  entry: CardActionEntry;
  deckId: string;
  api: DeckCardApi;
  onChange: () => void;
  onAddMore?: () => void;
  onCopyName?: (name: string) => void;
}): CardAction[] {
  const { entry, deckId, api, onChange, onAddMore, onCopyName } = opts;
  const { card_id: cardId, board, quantity, name } = entry;
  const after = (p: Promise<unknown>) => Promise.resolve(p).then(onChange);
  const actions: CardAction[] = [];

  // The commander is fixed by the commander picker: no quantity or board edits.
  if (board !== "command") {
    actions.push({
      id: "add-one",
      label: "Add One",
      shortcut: "Alt+1",
      run: () => after(api.addCard(deckId, cardId, board)),
    });
    actions.push({
      id: "add-more",
      label: "Add More…",
      run: () => onAddMore?.(),
    });
    actions.push({
      id: "remove-one",
      label: quantity > 1 ? "Remove One" : "Remove",
      shortcut: "Alt+2",
      danger: true,
      run: () => after(api.setCardQuantity(deckId, cardId, board, quantity - 1)),
    });
    if (quantity > 1) {
      actions.push({
        id: "remove-all",
        label: "Remove All",
        danger: true,
        run: () => after(api.removeCard(deckId, cardId, board)),
      });
    }
    for (const target of MOVE_TARGETS) {
      if (target.board === board) continue;
      actions.push({
        id: `move-${target.board}`,
        label: target.label,
        shortcut: target.shortcut,
        run: () => after(api.moveCard(deckId, cardId, board, target.board)),
      });
    }
  }

  actions.push({
    id: "copy-name",
    label: "Copy Card Name",
    run: () => onCopyName?.(name),
  });

  return actions;
}

// findShortcutAction maps an Alt+digit chord to its action, so the shortcuts
// work on a hovered row whether or not the menu is open.
//
// @spec DECK-092
export function findShortcutAction(actions: CardAction[], chord: string): CardAction | undefined {
  return actions.find((a) => a.shortcut === chord);
}
