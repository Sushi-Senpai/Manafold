"use client";

// One card-search result row: enough to identify the card at a glance (name,
// mana cost, type line, colour identity), a one-click Add, an "N in deck"
// reflection when the card is already in the list, and a transient confirmation
// after an add.
//
// @spec DECK-080, DECK-081, DECK-083, DECK-084

import type { CardSummary } from "@/lib/api";
import { HoverCardName } from "./HoverCardName";
import { ManaCost, ColorIdentity } from "./ManaSymbols";

export function SearchResultRow({
  card,
  inDeck,
  focused,
  justAdded,
  onAdd,
  onPointerFocus,
}: {
  card: CardSummary;
  inDeck: number;
  focused: boolean;
  justAdded: boolean;
  onAdd: () => void;
  onPointerFocus: () => void;
}) {
  return (
    <li
      role="option"
      aria-selected={focused}
      onMouseMove={onPointerFocus}
      className={`flex items-center gap-2 rounded-md px-2 py-1.5 text-sm ${
        focused ? "bg-primary/10 ring-1 ring-primary/30" : "hover:bg-primary/5"
      }`}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <HoverCardName card={card} className="truncate font-medium text-foreground">
            {card.name}
          </HoverCardName>
          <ManaCost cost={card.mana_cost} />
        </div>
        <div className="flex items-center gap-1.5 text-xs text-muted">
          <span className="truncate">{card.type_line}</span>
          <ColorIdentity colors={card.color_identity} />
        </div>
      </div>

      {inDeck > 0 && (
        <span
          className="shrink-0 rounded bg-surface-2 px-1.5 py-0.5 text-[10px] font-medium text-muted"
          title={`${inDeck} already in this deck`}
        >
          {inDeck} in deck
        </span>
      )}

      <button
        type="button"
        onClick={onAdd}
        aria-label={`Add ${card.name}`}
        className={`shrink-0 rounded-md border px-2 py-1 text-xs font-medium transition ${
          justAdded
            ? "border-success/40 bg-success/10 text-success"
            : "border-border hover:border-primary hover:text-primary"
        }`}
      >
        {justAdded ? "Added ✓" : "Add"}
      </button>
    </li>
  );
}
