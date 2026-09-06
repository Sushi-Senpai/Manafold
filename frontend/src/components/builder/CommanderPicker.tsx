"use client";

// Commander picker: shows the assigned commander (and partner), and an
// is:commander-filtered search to (re)assign one. The assigned names and every
// result option are hoverable card previews like the rest of the builder.
//
// @spec DECK-002, DECK-076

import { useEffect, useState } from "react";

import { api, ApiError, type CardSummary, type DeckDetail } from "@/lib/api";
import { HoverCardName } from "./HoverCardName";
import { ColorIdentity } from "./ManaSymbols";

export function CommanderPicker({
  detail,
  deckId,
  onChange,
}: {
  detail: DeckDetail;
  deckId: string;
  onChange: () => void;
}) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<CardSummary[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const term = q.trim();
    let cancelled = false;
    const handle = setTimeout(() => {
      if (cancelled) return;
      if (term.length < 2) {
        setResults([]);
        return;
      }
      api
        .searchCards(`is:commander ${term}`)
        .then((r) => {
          if (!cancelled) setResults(r.cards.slice(0, 8));
        })
        .catch(() => {
          if (!cancelled) setResults([]);
        });
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [q]);

  async function choose(card: CardSummary) {
    setBusy(true);
    try {
      await api.setCommander(deckId, card.id);
      setQ("");
      setResults([]);
      onChange();
    } catch (e) {
      alert(e instanceof ApiError ? e.message : "Could not set commander");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground/50">Commander</h2>
      {detail.commander ? (
        <p className="mt-2 text-sm">
          <HoverCardName card={detail.commander} className="font-medium">
            {detail.commander.name}
          </HoverCardName>
          {detail.partner && (
            <>
              <span className="text-foreground/50"> &amp; </span>
              <HoverCardName card={detail.partner} className="font-medium">
                {detail.partner.name}
              </HoverCardName>
            </>
          )}
        </p>
      ) : (
        <p className="mt-2 text-sm text-foreground/50">No commander assigned.</p>
      )}
      <div className="relative mt-3">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search legendary creatures…"
          aria-label="Commander search"
          disabled={busy}
          className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
        />
        {results.length > 0 && (
          <ul className="absolute z-10 mt-1 max-h-64 w-full overflow-auto rounded-lg border border-border bg-surface shadow-lg">
            {results.map((card) => (
              <li key={card.id}>
                <button
                  type="button"
                  onClick={() => choose(card)}
                  className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm hover:bg-primary/10"
                >
                  <HoverCardName card={card} className="truncate">
                    {card.name}
                  </HoverCardName>
                  <ColorIdentity colors={card.color_identity} className="shrink-0" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
