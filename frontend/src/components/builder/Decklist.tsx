"use client";

// The decklist: the board split (Commander / Mainboard / Considering /
// Sideboard) is the outer level; within each non-command board, entries are
// grouped by primary card type with a copy count in each header
// ("Creatures (15)"). Each row is a DecklistRow. This component owns the single
// "which row's action menu is open" id so at most one is ever open, and closes
// it on any scroll or navigation (outside-click / Escape are the menu's own).
//
// @spec DECK-007, DECK-087, DECK-090, DECK-092, DECK-094

import { useEffect, useState } from "react";

import { api, ApiError, type DeckDetail, type DeckEntry } from "@/lib/api";
import {
  BOARD_ORDER,
  BOARD_LABELS,
  boardCount,
  groupByType,
  activeShortcutRow,
  explainFitLabel,
  type BoardName,
} from "@/lib/deck";
import { DecklistRow } from "./DecklistRow";

export function Decklist({
  detail,
  deckId,
  onChange,
}: {
  detail: DeckDetail;
  deckId: string;
  onChange: () => void;
}) {
  const [openMenuEntryId, setOpenMenuEntryId] = useState<string | null>(null);

  // The single row that owns the Alt+1..4 chords: the pointer row, or failing
  // that the keyboard-focus row (activeShortcutRow). Tracked as two ids so a
  // chord is never bound on two rows at once.
  const [pointerRowId, setPointerRowId] = useState<string | null>(null);
  const [focusRowId, setFocusRowId] = useState<string | null>(null);
  const shortcutRowId = activeShortcutRow(pointerRowId, focusRowId);

  // One menu at a time; close it on any scroll (capture, so a scroll inside a
  // nested pane counts too) and on navigation.
  useEffect(() => {
    if (openMenuEntryId == null) return;
    const close = () => setOpenMenuEntryId(null);
    window.addEventListener("scroll", close, { capture: true, passive: true });
    window.addEventListener("resize", close);
    window.addEventListener("popstate", close);
    return () => {
      window.removeEventListener("scroll", close, { capture: true });
      window.removeEventListener("resize", close);
      window.removeEventListener("popstate", close);
    };
  }, [openMenuEntryId]);

  const empty = BOARD_ORDER.every((b) => (detail.boards[b as BoardName] ?? []).length === 0);

  function row(entry: DeckEntry) {
    return (
      <DecklistRow
        key={entry.entry_id}
        entry={entry}
        deckId={deckId}
        onChange={onChange}
        menuOpen={openMenuEntryId === entry.entry_id}
        onOpenMenu={() => setOpenMenuEntryId(entry.entry_id)}
        onCloseMenu={() => setOpenMenuEntryId(null)}
        shortcutActive={shortcutRowId === entry.entry_id}
        onPointerChange={(over) =>
          setPointerRowId((cur) =>
            over ? entry.entry_id : cur === entry.entry_id ? null : cur,
          )
        }
        onFocusChange={(within) =>
          setFocusRowId((cur) =>
            within ? entry.entry_id : cur === entry.entry_id ? null : cur,
          )
        }
        footer={
          entry.board === "main" || entry.board === "command" ? (
            <ExplainFit deckId={deckId} cardId={entry.card_id} />
          ) : undefined
        }
      />
    );
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Decklist</h2>
      <div className="mt-3 flex flex-col gap-6">
        {BOARD_ORDER.map((board) => {
          const entries = detail.boards[board as BoardName] ?? [];
          if (entries.length === 0) return null;
          const isCommand = board === "command";
          return (
            <section key={board}>
              <h3 className="text-xs font-semibold uppercase tracking-wide text-foreground/40">
                {BOARD_LABELS[board as BoardName]} · {boardCount(entries)}
              </h3>
              {isCommand ? (
                <ul className="mt-2 flex flex-col">{entries.map(row)}</ul>
              ) : (
                groupByType(entries).map((group) => (
                  <div key={group.type} className="mt-3">
                    <p className="text-xs font-medium text-foreground/50">
                      {group.type} ({group.count})
                    </p>
                    <ul className="mt-1 flex flex-col">{group.entries.map(row)}</ul>
                  </div>
                ))
              )}
            </section>
          );
        })}
        {empty && (
          <p className="text-sm text-foreground/40">Empty. Set a commander and add some cards.</p>
        )}
      </div>
    </div>
  );
}

// ---- ai explain fit --------------------------------------------------

// Single-card fit blurb (@spec AI-021): a per-row action that asks the model why
// this card belongs alongside the deck's commander. The card is already on a
// deck board, so the server accepts it; the blurb, its loading state, and any
// error render inline under the row via DecklistRow's footer slot.
function ExplainFit({ deckId, cardId }: { deckId: string; cardId: string }) {
  const [text, setText] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.explainCard(deckId, cardId);
      setText(res.explanation);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Could not explain this card");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="mt-0.5">
      <button
        type="button"
        onClick={run}
        disabled={loading}
        className="text-xs text-foreground/40 hover:text-primary disabled:opacity-50"
      >
        {explainFitLabel({ loading, hasBlurb: text !== null })}
      </button>
      {error && <p className="mt-0.5 text-xs text-danger">{error}</p>}
      {text && <p className="mt-0.5 text-xs text-foreground/70">{text}</p>}
    </div>
  );
}
