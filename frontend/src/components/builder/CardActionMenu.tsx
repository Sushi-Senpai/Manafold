"use client";

// The hover action menu for a decklist row: a small keyboard-navigable list of
// the context-valid actions (built by lib/cardActions) with their hover
// shortcuts shown. Rendered inline beside the row (no portal) so it inherits the
// builder palette; the row owns when it opens and the while-hovered shortcuts.
//
// @spec DECK-090, DECK-092

import { useEffect, useRef, useState } from "react";

import type { CardAction } from "@/lib/cardActions";

export function CardActionMenu({
  actions,
  onRun,
  onClose,
  addMore,
}: {
  actions: CardAction[];
  onRun: (action: CardAction) => void;
  onClose: () => void;
  addMore?: { open: boolean; onConfirm: (n: number) => void };
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [cursor, setCursor] = useState(0);
  const [amount, setAmount] = useState(2);

  useEffect(() => {
    ref.current?.querySelectorAll<HTMLButtonElement>("[data-item]")[cursor]?.focus();
  }, [cursor]);

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setCursor((c) => Math.min(c + 1, actions.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setCursor((c) => Math.max(c - 1, 0));
    }
  }

  return (
    <div
      ref={ref}
      role="menu"
      aria-label="Card actions"
      onKeyDown={onKeyDown}
      className="absolute right-0 top-full z-50 mt-1 w-52 overflow-hidden rounded-lg border border-border bg-surface py-1 text-sm shadow-xl"
    >
      {actions.map((action) => (
        <button
          key={action.id}
          data-item
          type="button"
          role="menuitem"
          onClick={() => onRun(action)}
          className={`flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left outline-none hover:bg-primary/10 focus-visible:bg-primary/10 ${
            action.danger ? "text-danger" : "text-foreground"
          }`}
        >
          <span>{action.label}</span>
          {action.shortcut && (
            <kbd className="rounded border border-border bg-surface-2 px-1 text-[10px] text-muted">
              {action.shortcut}
            </kbd>
          )}
        </button>
      ))}

      {addMore?.open && (
        <div className="mt-1 flex items-center gap-2 border-t border-border px-3 py-2">
          <input
            type="number"
            min={1}
            value={amount}
            onChange={(e) => setAmount(Math.max(1, Number(e.target.value) || 1))}
            aria-label="Copies to add"
            className="w-14 rounded border border-border bg-background px-1.5 py-1 text-xs"
          />
          <button
            type="button"
            onClick={() => addMore.onConfirm(amount)}
            className="rounded-md bg-primary px-2 py-1 text-xs font-medium text-primary-ink"
          >
            Add {amount}
          </button>
        </div>
      )}
    </div>
  );
}
