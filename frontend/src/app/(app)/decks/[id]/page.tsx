"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import {
  api,
  ApiError,
  type CardSummary,
  type DeckDetail,
  type DeckStats,
  type ImportFormat,
  type ImportPreview,
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
import { curveRows, pipRows, categoryRows } from "@/lib/deckstats";

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

      <section className="grid gap-6 md:grid-cols-2">
        <ImportExportPanel deckId={id} onImported={reload} />
        <StatsPanel deckId={id} detail={detail} />
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

// ---- import / export ------------------------------------------------

const IMPORT_FORMATS: { value: ImportFormat; label: string }[] = [
  { value: "plaintext", label: "Plain text" },
  { value: "mtga", label: "MTG Arena" },
  { value: "moxfield", label: "Moxfield" },
  { value: "archidekt", label: "Archidekt" },
];

// @spec PORT-001, PORT-004, PORT-006, PORT-007
function ImportExportPanel({
  deckId,
  onImported,
}: {
  deckId: string;
  onImported: () => void;
}) {
  const [format, setFormat] = useState<ImportFormat>("plaintext");
  const [raw, setRaw] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [exported, setExported] = useState("");

  async function doParse() {
    setBusy(true);
    setMsg(null);
    try {
      const p = await api.parseImport(deckId, format, raw);
      setPreview(p);
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Parse failed");
    } finally {
      setBusy(false);
    }
  }

  async function doApply() {
    if (!preview) return;
    setBusy(true);
    setMsg(null);
    try {
      await api.applyImport(deckId, preview.import_id);
      setPreview(null);
      setRaw("");
      setMsg("Imported.");
      onImported();
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Apply failed");
    } finally {
      setBusy(false);
    }
  }

  async function doExport(fmt: "plaintext" | "mtga") {
    setMsg(null);
    try {
      setExported(await api.exportDeck(deckId, fmt));
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Export failed");
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">
        Import / export
      </h2>

      <div className="mt-3 flex items-center gap-2">
        <select
          value={format}
          onChange={(e) => setFormat(e.target.value as ImportFormat)}
          className="rounded-lg border border-border bg-background px-2 py-1.5 text-sm"
        >
          {IMPORT_FORMATS.map((f) => (
            <option key={f.value} value={f.value}>
              {f.label}
            </option>
          ))}
        </select>
        <button
          onClick={doParse}
          disabled={busy || raw.trim() === ""}
          className="rounded-md border border-border px-2 py-1.5 text-xs font-medium transition hover:border-primary hover:text-primary disabled:opacity-40"
        >
          Preview import
        </button>
      </div>

      <textarea
        value={raw}
        onChange={(e) => setRaw(e.target.value)}
        rows={5}
        placeholder={"Commander:\n1 Atraxa, Praetors' Voice\n\nDeck\n1 Sol Ring\n…"}
        className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary"
      />

      {preview && (
        <div className="mt-3 rounded-md border border-border bg-surface-2 p-3 text-xs">
          <p className="font-medium">
            {preview.resolved.length} resolved · {preview.unresolved.length} unresolved
            {preview.rejected.length > 0 && ` · ${preview.rejected.length} unreadable`}
          </p>
          {preview.unresolved.length > 0 && (
            <ul className="mt-1 list-disc pl-4 text-warning">
              {preview.unresolved.map((u, i) => (
                <li key={i}>
                  {u.quantity}× {u.name}
                </li>
              ))}
            </ul>
          )}
          <button
            onClick={doApply}
            disabled={busy || preview.resolved.length === 0}
            className="mt-2 rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-ink transition hover:opacity-90 disabled:opacity-40"
          >
            Add {preview.resolved.length} cards to the deck
          </button>
        </div>
      )}

      <div className="mt-3 flex items-center gap-2">
        <span className="text-xs text-muted">Export:</span>
        <button
          onClick={() => doExport("plaintext")}
          className="rounded-md border border-border px-2 py-1 text-xs font-medium hover:border-primary hover:text-primary"
        >
          Plain text
        </button>
        <button
          onClick={() => doExport("mtga")}
          className="rounded-md border border-border px-2 py-1 text-xs font-medium hover:border-primary hover:text-primary"
        >
          MTG Arena
        </button>
      </div>
      {exported && (
        <textarea
          readOnly
          value={exported}
          rows={5}
          className="mt-2 w-full rounded-lg border border-border bg-surface-2 px-3 py-2 font-mono text-xs"
        />
      )}
      {msg && <p className="mt-2 text-xs text-muted">{msg}</p>}
    </div>
  );
}

// ---- deck stats --------------------------------------------------------

// @spec DECK-051, DECK-052
function StatsPanel({ deckId, detail }: { deckId: string; detail: DeckDetail }) {
  const [stats, setStats] = useState<DeckStats | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Refetch whenever the deck's cards or commander change.
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

  if (error) return <div className="rounded-lg border border-border bg-surface p-4 text-sm text-danger">{error}</div>;
  if (!stats) return <div className="rounded-lg border border-border bg-surface p-4 text-sm text-muted">Loading stats…</div>;

  const curve = curveRows(stats);
  const curveMax = Math.max(1, ...curve.map((r) => r.count));
  const pips = pipRows(stats);
  const cats = categoryRows(stats);

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Deck stats</h2>

      <dl className="mt-3 grid grid-cols-3 gap-2 text-sm">
        <div>
          <dt className="text-xs text-muted">Lands</dt>
          <dd className="font-mono">{stats.land_count}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Non-land</dt>
          <dd className="font-mono">{stats.nonland_count}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Avg MV</dt>
          <dd className="font-mono">{stats.avg_mana_value.toFixed(2)}</dd>
        </div>
      </dl>

      <h3 className="mt-4 text-xs font-semibold uppercase tracking-wide text-muted">Mana curve</h3>
      <div className="mt-2 flex items-end gap-1" aria-label="mana curve">
        {curve.map((r) => (
          <div key={r.bucket} className="flex flex-1 flex-col items-center gap-1">
            <div
              className="w-full rounded-t bg-primary/70"
              style={{ height: `${(r.count / curveMax) * 64 + 2}px` }}
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
    </div>
  );
}
