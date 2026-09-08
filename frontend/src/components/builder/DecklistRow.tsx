"use client";

// One decklist entry row: a card name that drives the image panel, its
// per-entry violation badges (colour identity / singleton — never hidden), a
// quantity stepper (non-command boards), a `⋯` control that opens the action
// menu, and the pointer/focus Alt+1..4 shortcuts. The menu's open state is owned
// by `Decklist` (one menu at a time); this row only asks it to open / close.
// The shortcuts are bound whenever the pointer is over the row or the keyboard
// focus is within it — never through the menu, which may not be rendered.
// `footer` is a slot other builder features hang per-row UI from.
//
// @spec DECK-076, DECK-086, DECK-090, DECK-092, DECK-093

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { api, ApiError, type DeckEntry } from "@/lib/api";
import { buildCardActions, findShortcutAction, type CardAction } from "@/lib/cardActions";
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
  menuOpen,
  onOpenMenu,
  onCloseMenu,
  footer,
}: {
  entry: DeckEntry;
  deckId: string;
  onChange: () => void;
  menuOpen: boolean;
  onOpenMenu: () => void;
  onCloseMenu: () => void;
  footer?: ReactNode;
}) {
  const kebabRef = useRef<HTMLButtonElement>(null);
  const [active, setActive] = useState(false); // pointer over OR focus within
  const [addMoreOpen, setAddMoreOpen] = useState(false);
  const [note, setNote] = useState<string | null>(null);

  const flash = useCallback((msg: string) => setNote(msg), []);

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

  const closeMenu = useCallback(() => {
    setAddMoreOpen(false);
    onCloseMenu();
  }, [onCloseMenu]);

  // Alt+1..4 on the row under the pointer or with keyboard focus-within, menu or
  // no menu, unless the caller is typing in a field.
  useEffect(() => {
    if (!active) return;
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
  }, [active, actions, run]);

  const incAction = actions.find((a) => a.id === "add-one");
  const decAction = actions.find((a) => a.id === "remove-one");
  const isCommand = entry.board === "command";

  return (
    <li
      className="group relative py-1 text-sm"
      onMouseEnter={() => setActive(true)}
      onMouseLeave={() => setActive(false)}
      onFocus={() => setActive(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setActive(false);
      }}
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
            ref={kebabRef}
            type="button"
            aria-label="Card actions"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={() => (menuOpen ? closeMenu() : onOpenMenu())}
            className="rounded px-1 leading-none text-muted transition hover:text-foreground"
          >
            ⋯
          </button>
        </span>
      </div>

      {menuOpen && (
        <CardActionMenu
          actions={actions}
          anchorRef={kebabRef}
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
