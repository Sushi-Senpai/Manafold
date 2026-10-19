"use client";

import { useEffect, useState } from "react";
import { api, ApiError, type CommanderSynergy, type DeckDetail } from "@/lib/api";
import {
  ATTRIBUTION_TEXT,
  ATTRIBUTION_URL,
  cardIdsInDeck,
  percent,
  sampleFooter,
  signedPercent,
} from "@/lib/synergy";
import { HoverCardName } from "./HoverCardName";

// The commander's top synergy cards — inclusion among public Archidekt decks
// for this commander versus its colour identity — with an add-to-main action
// and the attribution Archidekt asked for. It reads the local database through
// GET /api/synergy/commanders/{cardId}; the backend never calls Archidekt on a
// request. Shown once the deck has a commander (SYN-043).
//
// @spec SYN-043
export function SynergyPanel({
  deckId,
  detail,
  onChange,
}: {
  deckId: string;
  detail: DeckDetail;
  onChange: () => void;
}) {
  const commanderId = detail.commander?.id ?? null;
  const [data, setData] = useState<{ forCommander: string; value: CommanderSynergy | null } | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [adding, setAdding] = useState<string | null>(null);

  useEffect(() => {
    if (!commanderId) return;
    let cancelled = false;
    api
      .getCommanderSynergy(commanderId)
      .then((value) => {
        if (!cancelled) {
          setError(null);
          setData({ forCommander: commanderId, value });
        }
      })
      .catch((e) => {
        if (cancelled) return;
        if (e instanceof ApiError && e.status === 404) {
          setError(null);
          setData({ forCommander: commanderId, value: null });
        } else {
          setError(e instanceof ApiError ? e.message : "Failed to load synergy data");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [commanderId]);

  if (!commanderId) return null;

  const loaded = data && data.forCommander === commanderId ? data : null;
  const inDeck = cardIdsInDeck(detail);

  async function add(cardId: string) {
    setAdding(cardId);
    setMessage(null);
    try {
      await api.addCard(deckId, cardId, "main");
      onChange();
    } catch (e) {
      setMessage(e instanceof ApiError ? e.message : "Could not add card");
    } finally {
      setAdding(null);
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Synergy</h2>

      {error && <p className="mt-3 text-sm text-danger">{error}</p>}
      {!loaded && !error && <p className="mt-3 text-sm text-muted">Loading synergy…</p>}
      {message && <p className="mt-3 text-sm text-danger">{message}</p>}

      {loaded && !loaded.value && (
        <p className="mt-3 text-sm text-muted">
          No synergy data for this commander yet. Data is gathered for a limited set of popular
          commanders.
        </p>
      )}

      {loaded?.value && (
        <>
          <ul className="mt-3 flex flex-col gap-2">
            {loaded.value.cards.map((s) => (
              <li key={s.card.id} className="flex items-start justify-between gap-3 text-sm">
                <div className="min-w-0">
                  <HoverCardName card={s.card} className="font-medium">
                    {s.card.name}
                  </HoverCardName>{" "}
                  <span className="text-xs text-muted">{s.card.type_line}</span>
                  <p className="text-xs text-foreground/70">
                    <span className="font-mono">{signedPercent(s.synergy)}</span> synergy ·{" "}
                    {percent(s.inclusion)} of decks
                  </p>
                </div>
                {inDeck.has(s.card.id) ? (
                  <span className="shrink-0 px-2 py-1 text-xs text-muted">In deck</span>
                ) : (
                  <button
                    type="button"
                    onClick={() => add(s.card.id)}
                    disabled={adding === s.card.id}
                    className="shrink-0 rounded-md border border-border px-2 py-1 text-xs disabled:opacity-50"
                  >
                    Add
                  </button>
                )}
              </li>
            ))}
          </ul>
          <p className="mt-3 text-xs text-muted">
            {sampleFooter(loaded.value)} ·{" "}
            <a
              href={loaded.value.attribution?.url || ATTRIBUTION_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="underline"
            >
              {loaded.value.attribution?.text || ATTRIBUTION_TEXT}
            </a>
          </p>
        </>
      )}
    </div>
  );
}
