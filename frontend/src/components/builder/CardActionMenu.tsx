"use client";

// The decklist row's action menu: a small keyboard-navigable list of the
// context-valid actions (built by lib/cardActions). It opens only when the row
// asks it to (never on hover — DECK-090), renders `position: fixed` with a
// placement computed by lib/menuPlacement so it never covers its own row or
// leaves the viewport (DECK-094), and reports outside-click / Escape back to the
// row so the row can close it. The row also closes it on scroll and navigation.
//
// @spec DECK-090, DECK-091, DECK-094

import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";

import type { CardAction } from "@/lib/cardActions";
import { computeMenuPlacement, type MenuPlacement } from "@/lib/menuPlacement";

export function CardActionMenu({
  actions,
  anchorRef,
  onRun,
  onClose,
  addMore,
}: {
  actions: CardAction[];
  anchorRef: RefObject<HTMLElement | null>;
  onRun: (action: CardAction) => void;
  onClose: () => void;
  addMore?: { open: boolean; onConfirm: (n: number) => void };
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [cursor, setCursor] = useState(0);
  const [amount, setAmount] = useState(2);
  const [placement, setPlacement] = useState<MenuPlacement | null>(null);

  // Measure the menu and the anchor, then place. Runs before paint so the menu
  // never flashes at 0,0.
  useLayoutEffect(() => {
    const anchorEl = anchorRef.current;
    if (!anchorEl || !ref.current) return;
    const a = anchorEl.getBoundingClientRect();
    const m = ref.current.getBoundingClientRect();
    setPlacement(
      computeMenuPlacement(
        { top: a.top, left: a.left, width: a.width, height: a.height },
        { width: m.width, height: m.height },
      ),
    );
  }, [anchorRef, actions.length, addMore?.open]);

  useEffect(() => {
    ref.current?.querySelectorAll<HTMLButtonElement>("[data-item]")[cursor]?.focus();
  }, [cursor]);

  // Outside-click and Escape dismissal. Scroll / navigation are the row's job
  // (it owns the open flag).
  useEffect(() => {
    function onDown(e: MouseEvent) {
      if (!ref.current) return;
      const t = e.target as Node;
      if (!ref.current.contains(t) && !anchorRef.current?.contains(t)) onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    }
    document.addEventListener("mousedown", onDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("mousedown", onDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [anchorRef, onClose]);

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
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
      className="fixed z-50 w-52 overflow-hidden rounded-lg border border-border bg-surface py-1 text-sm shadow-xl"
      style={
        placement
          ? { left: placement.left, top: placement.top }
          : { left: 0, top: 0, visibility: "hidden" }
      }
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
