"use client";

// One decklist entry row: a hoverable card name, its per-entry violation badges
// (colour identity / singleton — never hidden), a quantity stepper (non-command
// boards), and the hover action menu with its while-hovered keyboard shortcuts.
// `footer` is a slot other builder features hang per-row UI from without
// re-touching this file.
//
// @spec DECK-076, DECK-086, DECK-090, DECK-092, DECK-093

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { api, ApiError, type DeckEntry } from "@/lib/api";
import { buildCardActions, findShortcutAction, type CardAction } from "@/lib/cardActions";
import { useCardPreview } from "./CardPreviewContext";
import { HoverCardName } from "./HoverCardName";
import { QuantityStepper } from "./QuantityStepper";
import { CardActionMenu } from "./CardActionMenu";

// Physical digit keys → shortcut chord, so Alt+1..4 fire regardless of the
// character the OS would otherwise produce (e.g. macOS Option+digit).
const SHORTCUT_CODE: Record<string, string> = {
  Digit1: "Alt+1",
  Digit2: "Alt+2",
  Digit3: "Alt+3",
  Digit4: "Alt+4",
};

function typingTarget(): boolean {
  const el = document.activeElement as HTMLElement | null;
  if (!el) return false;
  return (
    el.tagName === "INPUT" ||
    el.tagName === "TEXTAREA" ||
    el.tagName === "SELECT" ||
    el.isContentEditable
  );
}

export function DecklistRow({
  entry,
  deckId,
  onChange,
  footer,
}: {
  entry: DeckEntry;
  deckId: string;
  onChange: () => void;
  footer?: ReactNode;
}) {
  const { hoverable } = useCardPreview();
  const isCommand = entry.board === "command";

  const rowRef = useRef<HTMLLIElement>(null);
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [hovered, setHovered] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [addMoreOpen, setAddMoreOpen] = useState(false);
  const [note, setNote] = useState<string | null>(null);

  const flash = useCallback((msg: string) => setNote(msg), []);

  // Auto-clear the transient row note.
  useEffect(() => {
    if (!note) return;
    const t = setTimeout(() => setNote(null), 1400);
    return () => clearTimeout(t);
  }, [note]);

  const run = useCallback(
    async (action: CardAction) => {
      try {
        await action.run();
      } catch (e) {
        flash(e instanceof ApiError ? e.message : "Action failed");
      }
    },
    [flash],
  );

  const actions = useMemo<CardAction[]>(
    () =>
      buildCardActions({
        entry,
        deckId,
        api,
        onChange,
        onAddMore: () => setAddMoreOpen(true),
        onCopyName: (name) => {
          void navigator.clipboard?.writeText(name);
          flash("Name copied");
        },
      }),
    [entry, deckId, onChange, flash],
  );

  const actionById = useCallback(
    (id: string) => actions.find((a) => a.id === id),
    [actions],
  );

  const closeMenu = useCallback(() => {
    setMenuOpen(false);
    setAddMoreOpen(false);
  }, []);

  // While the row is hovered, Alt+1..4 fire their action even with the menu
  // closed — unless the caller is typing in a field.
  useEffect(() => {
    if (!hovered || !hoverable) return;
    function onKey(e: KeyboardEvent) {
      if (!e.altKey) return;
      const chord = SHORTCUT_CODE[e.code];
      if (!chord || typingTarget()) return;
      const action = findShortcutAction(actions, chord);
      if (!action) return;
      e.preventDefault();
      void run(action);
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [hovered, hoverable, actions, run]);

  useEffect(
    () => () => {
      if (hoverTimer.current) clearTimeout(hoverTimer.current);
    },
    [],
  );

  function onEnter() {
    if (!hoverable) return;
    setHovered(true);
    hoverTimer.current = setTimeout(() => setMenuOpen(true), 220);
  }
  function onLeave() {
    setHovered(false);
    if (hoverTimer.current) clearTimeout(hoverTimer.current);
    // Leave the menu open if focus moved into it (keyboard use).
    requestAnimationFrame(() => {
      if (!rowRef.current?.contains(document.activeElement)) closeMenu();
    });
  }

  const incAction = actionById("add-one");
  const decAction = actionById("remove-one");

  return (
    <li
      ref={rowRef}
      className="group relative py-1 text-sm"
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1">
          {isCommand && entry.quantity > 1 && (
            <span className="text-foreground/50">{entry.quantity}× </span>
          )}
          <HoverCardName card={entry} className="truncate font-medium">
            {entry.name}
          </HoverCardName>
          {entry.color_identity_violation && (
            <span className="ml-1 shrink-0 rounded bg-red-500/15 px-1.5 py-0.5 text-[10px] font-medium text-red-600">
              outside identity
              {entry.offending_colors.length > 0 && `: ${entry.offending_colors.join("")}`}
            </span>
          )}
          {entry.singleton_violation && (
            <span className="ml-1 shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-700">
              singleton
            </span>
          )}
        </span>

        <span className="flex shrink-0 items-center gap-1.5">
          {note && <span className="text-[10px] text-muted">{note}</span>}
          {!isCommand && incAction && decAction && (
            <QuantityStepper
              quantity={entry.quantity}
              canDecrement
              onIncrement={() => void run(incAction)}
              onDecrement={() => void run(decAction)}
            />
          )}
          <button
            type="button"
            aria-label="Card actions"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((o) => !o)}
            className={`rounded px-1 leading-none text-muted transition hover:text-foreground ${
              hoverable && !menuOpen
                ? "opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                : ""
            }`}
          >
            ⋯
          </button>
        </span>
      </div>

      {menuOpen && (
        <CardActionMenu
          actions={actions}
          onRun={(action) => {
            void run(action);
            if (action.id !== "add-more") closeMenu();
          }}
          onClose={closeMenu}
          addMore={{
            open: addMoreOpen,
            onConfirm: (n) => {
              void run({
                id: "add-more-confirm",
                label: "",
                run: () =>
                  api
                    .setCardQuantity(deckId, entry.card_id, entry.board, entry.quantity + n)
                    .then(onChange),
              });
              closeMenu();
            },
          }}
        />
      )}

      {footer}
    </li>
  );
}
