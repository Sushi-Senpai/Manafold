"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import {
  api,
  ApiError,
  type AISuggestResponse,
  type DeckDetail,
  type DeckStats,
  type ValidationReport,
} from "@/lib/api";
import { formatValidationStrip, formatSuggestionsFooter } from "@/lib/deck";
import { curveRows, pipRows, categoryRows } from "@/lib/deckstats";
import { ActiveCardProvider } from "@/components/builder/ActiveCardContext";
import { CardImagePanel } from "@/components/builder/CardImagePanel";
import { HoverCardName } from "@/components/builder/HoverCardName";
import { CommanderPicker } from "@/components/builder/CommanderPicker";
import { CardSearch } from "@/components/builder/CardSearch";
import { Decklist } from "@/components/builder/Decklist";
import { ToolsMenu } from "@/components/builder/ToolsMenu";

// The builder page. It loads the deck + validation report and lays out the
// three-column workspace (DECK-096): a sticky left column with the card-image
// panel (DECK-070) and a collapsible stats section, a center column for the
// type-grouped decklist, and a right column with AI suggestions above card
// search. A slim legality summary is pinned under the deck header so it stays
// visible while building. Import/export lives behind the header Tools menu
// (DECK-095). It collapses to one column below the `xl` breakpoint.
//
// @spec DECK-004, DECK-007, DECK-087, DECK-096
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
    <ActiveCardProvider>
      <div className="flex flex-col gap-4">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold">{detail.name}</h1>
            <span className="text-sm text-foreground/50">
              Colour identity:{" "}
              {detail.color_identity.length > 0 ? detail.color_identity.join("") : "—"}
            </span>
          </div>
          <ToolsMenu deckId={id} onImported={reload} />
        </header>

        <LegalitySummary report={report} />

        <CommanderPicker detail={detail} deckId={id} onChange={reload} />

        <div className="flex flex-col gap-4 xl:flex-row xl:items-start">
          <aside className="order-3 flex flex-col gap-4 xl:order-1 xl:sticky xl:top-4 xl:max-h-[calc(100vh-2rem)] xl:w-[320px] xl:shrink-0 xl:overflow-y-auto">
            <CardImagePanel commander={detail.commander} />
            <StatsPanel deckId={id} detail={detail} />
          </aside>

          <div className="order-2 min-w-0 flex-1">
            <Decklist detail={detail} deckId={id} onChange={reload} />
          </div>

          <div className="order-1 flex flex-col gap-4 xl:order-3 xl:w-[380px] xl:shrink-0">
            <SuggestionsPanel deckId={id} detail={detail} onChange={reload} />
            <CardSearch deckId={id} detail={detail} onAdded={reload} />
          </div>
        </div>
      </div>
    </ActiveCardProvider>
  );
}

// ---- legality summary ----------------------------------------------

// A slim always-visible reading of the /validation report, pinned under the
// deck header so it stays in view while the columns scroll (DECK-096). Replaces
// the old fixed-to-the-bottom strip.
function LegalitySummary({ report }: { report: ValidationReport }) {
  const lines = formatValidationStrip(report);
  return (
    <div
      className={`sticky top-0 z-30 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border px-4 py-2 text-sm ${
        report.legal
          ? "border-success/30 bg-success/10 text-success"
          : "border-danger/30 bg-danger/10 text-danger"
      }`}
    >
      <span className="font-semibold uppercase tracking-wide">
        {report.legal ? "Legal" : "Not legal"}
      </span>
      {lines.map((line, i) => (
        <span key={i} className="text-foreground/70">
          {line}
        </span>
      ))}
    </div>
  );
}

// ---- deck stats --------------------------------------------------------

// @spec DECK-051, DECK-052
function StatsPanel({ deckId, detail }: { deckId: string; detail: DeckDetail }) {
  const [stats, setStats] = useState<DeckStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  const signature = JSON.stringify({
    c: detail.commander?.id ?? null,
    p: detail.partner?.id ?? null,
    b: Object.fromEntries(
      Object.entries(detail.boards).map(([k, v]) => [
        k,
        v.map((e) => `${e.card_id}:${e.quantity}:${e.category ?? ""}`).join(","),
      ]),
    ),
  });

  useEffect(() => {
    let cancelled = false;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setError(null);
    api
      .getDeckStats(deckId)
      .then((s) => {
        if (!cancelled) setStats(s);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "Failed to load stats");
      });
    return () => {
      cancelled = true;
    };
  }, [deckId, signature]);

  const curve = stats ? curveRows(stats) : [];
  const curveMax = Math.max(1, ...curve.map((r) => r.count));
  const pips = stats ? pipRows(stats) : [];
  const cats = stats ? categoryRows(stats) : [];

  return (
    <div className="rounded-lg border border-border bg-surface">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left"
      >
        <span className="text-sm font-semibold uppercase tracking-wide text-muted">Deck stats</span>
        <span className="flex items-center gap-2 text-xs text-muted">
          {stats && (
            <span className="font-mono">
              {stats.land_count}L · {stats.nonland_count}N · {stats.avg_mana_value.toFixed(2)} MV
            </span>
          )}
          <span aria-hidden>{open ? "▾" : "▸"}</span>
        </span>
      </button>

      {open && (
        <div className="border-t border-border px-4 pb-4 pt-3">
          {error && <p className="text-sm text-danger">{error}</p>}
          {!stats && !error && <p className="text-sm text-muted">Loading stats…</p>}
          {stats && (
            <>
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">Mana curve</h3>
              <div className="mt-2 flex items-end gap-1" aria-label="mana curve">
                {curve.map((r) => (
                  <div key={r.bucket} className="flex flex-1 flex-col items-center gap-1">
                    <div
                      className="w-full rounded-t bg-primary/70"
                      style={{ height: `${(r.count / curveMax) * 56 + 2}px` }}
                      title={`MV ${r.bucket}: ${r.count}`}
                    />
                    <span className="text-[10px] text-muted">{r.bucket}</span>
                  </div>
                ))}
              </div>

              {pips.length > 0 && (
                <>
                  <h3 className="mt-4 text-xs font-semibold uppercase tracking-wide text-muted">
                    Colour pips vs sources
                  </h3>
                  <ul className="mt-2 space-y-1 text-xs">
                    {pips.map((p) => (
                      <li key={p.color} className="flex justify-between">
                        <span className="font-mono">{p.color}</span>
                        <span className="text-muted">
                          {p.pips} pip{p.pips === 1 ? "" : "s"} · {p.sources} source
                          {p.sources === 1 ? "" : "s"}
                        </span>
                      </li>
                    ))}
                  </ul>
                </>
              )}

              {cats.length > 0 && (
                <>
                  <h3 className="mt-4 text-xs font-semibold uppercase tracking-wide text-muted">
                    Categories vs rules-of-thumb
                  </h3>
                  <ul className="mt-2 space-y-1 text-xs">
                    {cats.map((c) => (
                      <li key={c.name} className="flex justify-between">
                        <span>{c.name}</span>
                        <span
                          className={
                            c.status === "under"
                              ? "text-warning"
                              : c.status === "over"
                                ? "text-info"
                                : "text-muted"
                          }
                        >
                          {c.count}
                          {c.min !== null && ` / ${c.min}–${c.max}`}
                        </span>
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ---- ai suggestions ----------------------------------------------------

// "Suggest & explain" (@spec AI-020): one button runs the deck through the
// model; every card shown here has already passed the server-side
// anti-hallucination gate. Placed at the top of the right column (DECK-096) so
// it is prominent, not buried. Still 503s locally without an API key — expected.
function SuggestionsPanel({
  deckId,
  detail,
  onChange,
}: {
  deckId: string;
  detail: DeckDetail;
  onChange: () => void;
}) {
  const [result, setResult] = useState<AISuggestResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [adding, setAdding] = useState<string | null>(null);

  const hasCommander = detail.commander !== null;

  async function run() {
    setLoading(true);
    setMessage(null);
    try {
      setResult(await api.suggestDeck(deckId));
    } catch (e) {
      setMessage(e instanceof ApiError ? e.message : "Suggestions failed");
    } finally {
      setLoading(false);
    }
  }

  async function add(cardId: string) {
    setAdding(cardId);
    try {
      await api.addCard(deckId, cardId, "main");
      setResult((r) =>
        r ? { ...r, suggestions: r.suggestions.filter((s) => s.card.id !== cardId) } : r,
      );
      onChange();
    } catch (e) {
      setMessage(e instanceof ApiError ? e.message : "Could not add card");
    } finally {
      setAdding(null);
    }
  }

  return (
    <div className="rounded-lg border border-primary/30 bg-primary/5 p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-primary">AI suggestions</h2>
        <button
          type="button"
          onClick={run}
          disabled={!hasCommander || loading}
          className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-ink disabled:opacity-50"
        >
          {loading ? "Thinking…" : result ? "Refresh" : "Suggest cards"}
        </button>
      </div>

      {!hasCommander && (
        <p className="mt-3 text-sm text-muted">Assign a commander to get suggestions.</p>
      )}
      {message && <p className="mt-3 text-sm text-danger">{message}</p>}

      {result && (
        <>
          {result.suggestions.length === 0 ? (
            <p className="mt-3 text-sm text-muted">No suggestions survived the legality check.</p>
          ) : (
            <ul className="mt-3 flex flex-col gap-2">
              {result.suggestions.map((s) => (
                <li key={s.card.id} className="flex items-start justify-between gap-3 text-sm">
                  <div>
                    <HoverCardName card={s.card} className="font-medium">
                      {s.card.name}
                    </HoverCardName>{" "}
                    <span className="text-xs text-muted">{s.card.type_line}</span>
                    <p className="text-xs text-foreground/70">{s.reason}</p>
                  </div>
                  <button
                    type="button"
                    onClick={() => add(s.card.id)}
                    disabled={adding === s.card.id}
                    className="shrink-0 rounded-md border border-border px-2 py-1 text-xs disabled:opacity-50"
                  >
                    Add
                  </button>
                </li>
              ))}
            </ul>
          )}
          <p className="mt-3 text-xs text-muted">{formatSuggestionsFooter(result)}</p>
        </>
      )}
    </div>
  );
}
