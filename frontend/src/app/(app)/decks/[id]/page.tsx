"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import {
  api,
  ApiError,
  type CardSummary,
  type DeckDetail,
  type ValidationReport,
} from "@/lib/api";
import {
  BOARD_ORDER,
  BOARD_LABELS,
  boardCount,
  groupByCategory,
  formatValidationStrip,
  type BoardName,
} from "@/lib/deck";

// The M1 builder: commander picker, card search + add, decklist grouped by
// board and category, and a live validation strip.
//
// @spec DECK-002, DECK-004, DECK-007, DECK-008
export default function BuilderPage() {
  const { id } = useParams<{ id: string }>();
  const [detail, setDetail] = useState<DeckDetail | null>(null);
  const [report, setReport] = useState<ValidationReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      const [d, r] = await Promise.all([api.getDeck(id), api.getValidation(id)]);
      setDetail(d);
      setReport(r);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Failed to load deck");
    }
  }, [id]);

  useEffect(() => {
    let cancelled = false;
    Promise.all([api.getDeck(id), api.getValidation(id)])
      .then(([d, r]) => {
        if (cancelled) return;
        setDetail(d);
        setReport(r);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "Failed to load deck");
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  if (error) return <p className="text-sm text-red-600">{error}</p>;
  if (!detail || !report) return <p className="text-sm text-foreground/40">Loading…</p>;

  return (
    <div className="flex flex-col gap-6 pb-24">
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-2xl font-semibold">{detail.name}</h1>
        <span className="text-sm text-foreground/50">
          Colour identity:{" "}
          {detail.color_identity.length > 0 ? detail.color_identity.join("") : "—"}
        </span>
      </header>

      <CommanderPicker detail={detail} deckId={id} onChange={reload} />

      <section className="grid gap-6 md:grid-cols-2">
        <CardSearch deckId={id} onAdded={reload} />
        <Decklist detail={detail} deckId={id} onChange={reload} />
      </section>

      <ValidationStrip report={report} />
    </div>
  );
}

// ---- commander picker ------------------------------------------------

function CommanderPicker({
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
      <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground/50">
        Commander
      </h2>
      {detail.commander ? (
        <p className="mt-2 text-sm">
          <span className="font-medium">{detail.commander.name}</span>
          {detail.partner && <span className="font-medium"> &amp; {detail.partner.name}</span>}
        </p>
      ) : (
        <p className="mt-2 text-sm text-foreground/50">No commander assigned.</p>
      )}
      <div className="relative mt-3">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search legendary creatures…"
          disabled={busy}
          className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
        />
        {results.length > 0 && (
          <ul className="absolute z-10 mt-1 max-h-64 w-full overflow-auto rounded-lg border border-border bg-surface shadow-lg">
            {results.map((card) => (
              <li key={card.id}>
                <button
                  onClick={() => choose(card)}
                  className="flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-primary/10"
                >
                  <span>{card.name}</span>
                  <span className="text-xs text-foreground/40">
                    {card.color_identity.join("") || "C"}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

// ---- card search --------------------------------------------------------

function CardSearch({ deckId, onAdded }: { deckId: string; onAdded: () => void }) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<CardSummary[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const term = q.trim();
    let cancelled = false;
    const handle = setTimeout(() => {
      if (cancelled) return;
      if (term === "") {
        setResults([]);
        setLoading(false);
        return;
      }
      setLoading(true);
      api
        .searchCards(term)
        .then((r) => {
          if (!cancelled) setResults(r.cards.slice(0, 20));
        })
        .catch(() => {
          if (!cancelled) setResults([]);
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
      alert(e instanceof ApiError ? e.message : "Could not add card");
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground/50">
        Add cards
      </h2>
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Name, or a query like  id:r t:instant cmc<=2"
        className="mt-3 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
      />
      <ul className="mt-3 flex max-h-[28rem] flex-col gap-1 overflow-auto">
        {loading && <li className="px-2 py-1 text-xs text-foreground/40">Searching…</li>}
        {!loading && q.trim() !== "" && results.length === 0 && (
          <li className="px-2 py-1 text-xs text-foreground/40">No matches.</li>
        )}
        {results.map((card) => (
          <li
            key={card.id}
            className="flex items-center justify-between rounded-md px-2 py-1.5 text-sm hover:bg-primary/5"
          >
            <span className="truncate">
              {card.name}{" "}
              <span className="text-xs text-foreground/40">
                {card.type_line}
                {card.color_identity.length > 0 && ` · ${card.color_identity.join("")}`}
              </span>
            </span>
            <button
              onClick={() => add(card)}
              className="ml-2 shrink-0 rounded-md border border-border px-2 py-1 text-xs font-medium transition hover:border-primary hover:text-primary"
            >
              Add
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

// ---- decklist ---------------------------------------------------------

function Decklist({
  detail,
  deckId,
  onChange,
}: {
  detail: DeckDetail;
  deckId: string;
  onChange: () => void;
}) {
  async function remove(cardId: string, board: string) {
    try {
      await api.removeCard(deckId, cardId, board);
      onChange();
    } catch (e) {
      alert(e instanceof ApiError ? e.message : "Could not remove card");
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground/50">Decklist</h2>
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
                    {group.entries.map((e) => (
                      <li
                        key={e.entry_id}
                        className="flex items-center justify-between gap-2 py-1 text-sm"
                      >
                        <span className="truncate">
                          {e.quantity > 1 && <span className="text-foreground/50">{e.quantity}× </span>}
                          {e.name}
                          {e.color_identity_violation && (
                            <span className="ml-2 rounded bg-red-500/15 px-1.5 py-0.5 text-[10px] font-medium text-red-600">
                              outside identity{e.offending_colors.length > 0 && `: ${e.offending_colors.join("")}`}
                            </span>
                          )}
                          {e.singleton_violation && (
                            <span className="ml-2 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-700">
                              singleton
                            </span>
                          )}
                        </span>
                        {board !== "command" && (
                          <button
                            onClick={() => remove(e.card_id, e.board)}
                            className="shrink-0 text-xs text-foreground/40 hover:text-red-600"
                          >
                            remove
                          </button>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          );
        })}
        {BOARD_ORDER.every((b) => (detail.boards[b as BoardName] ?? []).length === 0) && (
          <p className="text-sm text-foreground/40">Empty. Set a commander and add some cards.</p>
        )}
      </div>
    </div>
  );
}

// ---- validation strip ------------------------------------------------

function ValidationStrip({ report }: { report: ValidationReport }) {
  const lines = formatValidationStrip(report);
  return (
    <div
      className={`fixed inset-x-0 bottom-0 border-t px-6 py-3 text-sm ${
        report.legal
          ? "border-green-600/30 bg-green-600/10 text-green-800"
          : "border-red-600/30 bg-red-600/10 text-red-800"
      }`}
    >
      <div className="mx-auto flex max-w-6xl flex-wrap gap-x-4 gap-y-1">
        {lines.map((line, i) => (
          <span key={i}>{line}</span>
        ))}
      </div>
    </div>
  );
}
