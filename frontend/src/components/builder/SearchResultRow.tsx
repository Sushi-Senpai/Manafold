"use client";

// One card-search result row. A click anywhere on the row adds the card to the
// `main` board (DECK-081) — there is no separate primary "Add" button. A small
// secondary `⋯` control (which a row click does not trigger) opens a two-item
// menu to add to `sideboard` or `maybe` instead. The row still shows "N in
// deck" when the card is already present and flashes a confirmation after an
// add. The name is a HoverCardName, so hovering / focusing the row drives the
// image panel.
//
// @spec DECK-080, DECK-081, DECK-083, DECK-084

import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";

import type { CardSummary } from "@/lib/api";
import { computeMenuPlacement, type MenuPlacement } from "@/lib/menuPlacement";
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
  onAdd: (board: "main" | "sideboard" | "maybe") => void;
  onPointerFocus: () => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const kebabRef = useRef<HTMLButtonElement>(null);

  return (
    <li
      role="option"
      aria-selected={focused}
      onMouseMove={onPointerFocus}
      onClick={() => onAdd("main")}
      className={`flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm ${
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

      {justAdded && (
        <span className="shrink-0 rounded-md border border-success/40 bg-success/10 px-2 py-1 text-[10px] font-medium text-success">
          Added ✓
        </span>
      )}

      <div className="relative shrink-0">
        <button
          ref={kebabRef}
          type="button"
          aria-label={`Add ${card.name} to another board`}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={(e) => {
            e.stopPropagation();
            setMenuOpen((o) => !o);
          }}
          className="rounded px-1 leading-none text-muted transition hover:text-foreground"
        >
          ⋯
        </button>
        {menuOpen && (
          <AddToBoardMenu
            anchorRef={kebabRef}
            onPick={(board) => {
              setMenuOpen(false);
              onAdd(board);
            }}
            onClose={() => setMenuOpen(false)}
          />
        )}
      </div>
    </li>
  );
}

// The secondary add-to-board menu: two items, positioned like the decklist
// action menu (fixed, flip/clamp) so it is never clipped by the result list's
// scroll pane. The anchor is passed in explicitly (like CardActionMenu) rather
// than found by DOM traversal, so it survives markup changes around the button.
function AddToBoardMenu({
  anchorRef,
  onPick,
  onClose,
}: {
  anchorRef: RefObject<HTMLElement | null>;
  onPick: (board: "sideboard" | "maybe") => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [placement, setPlacement] = useState<MenuPlacement | null>(null);

  useLayoutEffect(() => {
    const anchor = anchorRef.current;
    if (!anchor || !ref.current) return;
    const a = anchor.getBoundingClientRect();
    const m = ref.current.getBoundingClientRect();
    setPlacement(
      computeMenuPlacement(
        { top: a.top, left: a.left, width: a.width, height: a.height },
        { width: m.width, height: m.height },
      ),
    );
  }, [anchorRef]);

  useEffect(() => {
    function onDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("mousedown", onDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("mousedown", onDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      role="menu"
      onClick={(e) => e.stopPropagation()}
      className="fixed z-50 w-44 overflow-hidden rounded-lg border border-border bg-surface py-1 text-sm shadow-xl"
      style={
        placement
          ? { left: placement.left, top: placement.top }
          : { left: 0, top: 0, visibility: "hidden" }
      }
    >
      <button
        type="button"
        role="menuitem"
        onClick={() => onPick("sideboard")}
        className="block w-full px-3 py-1.5 text-left hover:bg-primary/10"
      >
        Add to Sideboard
      </button>
      <button
        type="button"
        role="menuitem"
        onClick={() => onPick("maybe")}
        className="block w-full px-3 py-1.5 text-left hover:bg-primary/10"
      >
        Add to Considering
      </button>
    </div>
  );
}
