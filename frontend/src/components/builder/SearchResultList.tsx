"use client";

// The card-search results list: renders enriched rows and owns keyboard
// navigation over them (Arrow keys move a cursor, Enter adds the row under the
// cursor to `main`) plus the short-lived "Added ✓" confirmation. It is
// deliberately presentational about data — the parent owns the query and the
// add call. A click anywhere on a row adds to `main`; a row's `⋯` adds to
// another board (SearchResultRow).
//
// @spec DECK-081, DECK-082, DECK-084

import { useCallback, useEffect, useRef, useState } from "react";

import type { CardSummary, DeckDetail } from "@/lib/api";
import { moveCursor, deckCardQuantity } from "@/lib/searchNav";
import { SearchResultRow } from "./SearchResultRow";

export function SearchResultList({
  cards,
  detail,
  loading,
  query,
  onAdd,
}: {
  cards: CardSummary[];
  detail: DeckDetail;
  loading: boolean;
  query: string;
  onAdd: (card: CardSummary, board: "main" | "sideboard" | "maybe") => void | Promise<void>;
}) {
  const [cursor, setCursor] = useState(-1);
  const [justAddedId, setJustAddedId] = useState<string | null>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const addedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Card ids whose add is still in flight. A whole-row click target makes a
  // rapid double-click or a stray second click easy; ignore a repeat add for a
  // card until its first add settles so one interaction never adds two copies.
  const inFlight = useRef<Set<string>>(new Set());

  // A fresh result set starts with no row focused — adjust during render when
  // the `cards` identity changes rather than in an effect.
  const [seenCards, setSeenCards] = useState(cards);
  if (seenCards !== cards) {
    setSeenCards(cards);
    setCursor(-1);
  }

  useEffect(() => {
    return () => {
      if (addedTimer.current) clearTimeout(addedTimer.current);
    };
  }, []);

  const flashAdded = useCallback((id: string) => {
    setJustAddedId(id);
    if (addedTimer.current) clearTimeout(addedTimer.current);
    addedTimer.current = setTimeout(() => setJustAddedId(null), 1200);
  }, []);

  const add = useCallback(
    (card: CardSummary, board: "main" | "sideboard" | "maybe") => {
      if (inFlight.current.has(card.id)) return;
      inFlight.current.add(card.id);
      flashAdded(card.id);
      Promise.resolve(onAdd(card, board)).finally(() => {
        inFlight.current.delete(card.id);
      });
    },
    [flashAdded, onAdd],
  );

  function onKeyDown(e: React.KeyboardEvent<HTMLUListElement>) {
    if (e.key === "Enter") {
      if (cursor >= 0 && cursor < cards.length) {
        e.preventDefault();
        add(cards[cursor], "main");
      }
      return;
    }
    const next = moveCursor(cursor, cards.length, e.key);
    if (next !== cursor) {
      e.preventDefault();
      setCursor(next);
      listRef.current?.querySelectorAll<HTMLLIElement>("[role=option]")[next]?.scrollIntoView({
        block: "nearest",
      });
    }
  }

  if (loading) {
    return <p className="mt-3 px-2 py-1 text-xs text-muted">Searching…</p>;
  }
  if (query.trim() !== "" && cards.length === 0) {
    return <p className="mt-3 px-2 py-1 text-xs text-muted">No matches.</p>;
  }
  if (cards.length === 0) {
    return null;
  }

  return (
    <ul
      ref={listRef}
      role="listbox"
      aria-label="Card search results"
      tabIndex={0}
      onKeyDown={onKeyDown}
      className="mt-3 flex max-h-[28rem] flex-col gap-1 overflow-auto rounded-md outline-none focus-visible:ring-1 focus-visible:ring-primary/40"
    >
      {cards.map((card, i) => (
        <SearchResultRow
          key={card.id}
          card={card}
          inDeck={deckCardQuantity(detail, card.id)}
          focused={i === cursor}
          justAdded={justAddedId === card.id}
          onAdd={(board) => add(card, board)}
          onPointerFocus={() => setCursor(i)}
        />
      ))}
    </ul>
  );
}
