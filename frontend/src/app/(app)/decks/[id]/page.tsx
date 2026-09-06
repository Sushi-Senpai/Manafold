"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import {
  api,
  ApiError,
  type AISuggestResponse,
  type DeckDetail,
  type DeckStats,
  type ImportFormat,
  type ImportPreview,
  type ValidationReport,
} from "@/lib/api";
import { formatValidationStrip, formatSuggestionsFooter } from "@/lib/deck";
import { curveRows, pipRows, categoryRows } from "@/lib/deckstats";
import { CardPreviewProvider } from "@/components/builder/CardPreviewContext";
import { CommanderPicker } from "@/components/builder/CommanderPicker";
import { CardSearch } from "@/components/builder/CardSearch";
import { Decklist } from "@/components/builder/Decklist";

// The builder page: it loads the deck + validation report, mounts the card
// hover-preview provider, and lays out the builder panels. The commander picker,
// card search, and decklist are their own components under
// components/builder/; the import/export, stats, and validation panels are
// still inline here.
//
// @spec DECK-004, DECK-007, DECK-008
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
    <CardPreviewProvider>
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
          <CardSearch deckId={id} detail={detail} onAdded={reload} />
          <Decklist detail={detail} deckId={id} onChange={reload} />
        </section>

        <section className="grid gap-6 md:grid-cols-2">
          <ImportExportPanel deckId={id} onImported={reload} />
          <StatsPanel deckId={id} detail={detail} />
        </section>

        <SuggestionsPanel deckId={id} detail={detail} onChange={reload} />

        <ValidationStrip report={report} />
      </div>
    </CardPreviewProvider>
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
    // Drop any stale error the moment the deck signature changes so the panel
    // shows "Loading stats…" rather than a previous failure while the refetch
    // is in flight.
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

// ---- ai suggestions ----------------------------------------------------

// "Suggest & explain" (@spec AI-020): one button runs the deck through the
// model; every card shown here has already passed the server-side
// anti-hallucination gate, so it is real, legal, in colour identity, and not
// already in the deck. Dropped cards are counted, never replaced.
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
    <div className="rounded-lg border border-border bg-surface p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">AI suggestions</h2>
        <button
          type="button"
          onClick={run}
          disabled={!hasCommander || loading}
          className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
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
                    <span className="font-medium">{s.card.name}</span>{" "}
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
