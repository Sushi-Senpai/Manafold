"use client";

// The decklist: entries grouped by board then functional category (helpers in
// lib/deck.ts). Each row is a DecklistRow, which owns its own quantity stepper
// and hover action menu against the deck API.
//
// @spec DECK-007

import { useState } from "react";

import { api, ApiError, type DeckDetail } from "@/lib/api";
import {
  BOARD_ORDER,
  BOARD_LABELS,
  boardCount,
  groupByCategory,
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
  const empty = BOARD_ORDER.every((b) => (detail.boards[b as BoardName] ?? []).length === 0);

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Decklist</h2>
      <div className="mt-3 flex flex-col gap-5">
        {BOARD_ORDER.map((board) => {
          const entries = detail.boards[board as BoardName] ?? [];
          if (entries.length === 0) return null;
          return (
            <div key={board}>
              <h3 className="text-xs font-semibold uppercase tracking-wide text-foreground/40">
                {BOARD_LABELS[board as BoardName]} · {boardCount(entries)}
              </h3>
              {groupByCategory(entries).map((group) => (
                <div key={group.category} className="mt-2">
                  {group.category !== "Uncategorised" && (
                    <p className="text-xs text-foreground/40">{group.category}</p>
                  )}
                  <ul className="flex flex-col">
                    {group.entries.map((entry) => (
                      <DecklistRow
                        key={entry.entry_id}
                        entry={entry}
                        deckId={deckId}
                        onChange={onChange}
                        footer={
                          entry.board === "main" || entry.board === "command" ? (
                            <ExplainFit deckId={deckId} cardId={entry.card_id} />
                          ) : undefined
                        }
                      />
                    ))}
                  </ul>
                </div>
              ))}
            </div>
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
