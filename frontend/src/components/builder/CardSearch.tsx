"use client";

// The builder's "Add cards" panel: a debounced query box over
// GET /api/cards/search (raw query passed straight through, so the id: / t: /
// cmc / o: / is:commander syntax keeps working) feeding the enriched,
// keyboard-navigable result list.
//
// @spec DECK-081, DECK-085

import { useEffect, useState } from "react";

import { api, ApiError, type CardSummary, type DeckDetail } from "@/lib/api";
import { SearchResultList } from "./SearchResultList";

export function CardSearch({
  deckId,
  detail,
  onAdded,
}: {
  deckId: string;
  detail: DeckDetail;
  onAdded: () => void;
}) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<CardSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const term = q.trim();
    let cancelled = false;
    const handle = setTimeout(() => {
      if (cancelled) return;
      if (term === "") {
        setResults([]);
        setLoading(false);
        setError(null);
        return;
      }
      setLoading(true);
      api
        .searchCards(term)
        .then((r) => {
          if (cancelled) return;
          setResults(r.cards.slice(0, 40));
          setError(null);
        })
        .catch((e) => {
          if (cancelled) return;
          setResults([]);
          setError(e instanceof ApiError ? e.message : "Search failed");
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [q]);

  async function add(card: CardSummary) {
    try {
      await api.addCard(deckId, card.id, "main");
      onAdded();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Could not add card");
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Add cards</h2>
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Name, or a query like  id:r t:instant cmc<=2"
        aria-label="Card search"
        className="mt-3 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
      />
      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
      <SearchResultList
        cards={results}
        detail={detail}
        loading={loading}
        query={q}
        onAdd={add}
      />
    </div>
  );
}
