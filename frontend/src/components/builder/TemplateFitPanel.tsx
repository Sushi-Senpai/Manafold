"use client";

import { useEffect, useState } from "react";
import {
  api,
  ApiError,
  type DeckDetail,
  type DeckTemplate,
  type TemplateFit,
} from "@/lib/api";
import { fitTone, formatFitRow } from "@/lib/templateFit";

const BRACKETS = [1, 2, 3, 4, 5] as const;

// The "your deck vs. target" panel (DECK-109). The player picks an archetype
// (and optionally a bracket) for the deck; every template row — card types,
// then roles — shows the deck's count against the target range, green in range
// and amber outside it. Templates and statuses come from the backend's one
// authoritative definition; nothing here carries a range of its own.
//
// @spec DECK-109
export function TemplateFitPanel({
  deckId,
  detail,
  onChange,
}: {
  deckId: string;
  detail: DeckDetail;
  onChange: () => void;
}) {
  const [templates, setTemplates] = useState<DeckTemplate[] | null>(null);
  const [fit, setFit] = useState<TemplateFit | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .getDeckTemplates()
      .then((t) => {
        if (!cancelled) setTemplates(t.templates);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "Failed to load templates");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const signature = JSON.stringify({
    a: detail.archetype,
    br: detail.bracket,
    c: detail.commander?.id ?? null,
    p: detail.partner?.id ?? null,
    m: detail.boards.main.map((e) => `${e.card_id}:${e.quantity}`).join(","),
  });

  useEffect(() => {
    let cancelled = false;
    api
      .getTemplateFit(deckId)
      .then((f) => {
        if (cancelled) return;
        setFit(f);
        setError(null);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "Failed to load target fit");
      });
    return () => {
      cancelled = true;
    };
  }, [deckId, signature]);

  async function save(patch: { archetype?: string; bracket?: number }) {
    setSaving(true);
    setError(null);
    try {
      await api.updateDeck(deckId, patch);
      onChange();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Could not save");
    } finally {
      setSaving(false);
    }
  }

  // A fit fetched for a different archetype (an archetype change whose refetch
  // failed or is still in flight) is never shown under the new label.
  const current = fit && fit.archetype === detail.archetype ? fit : null;
  const typeRows = current?.rows.filter((r) => r.kind === "type") ?? [];
  const roleRows = current?.rows.filter((r) => r.kind === "role") ?? [];

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Deck vs. target</h2>

      <div className="mt-3 flex flex-wrap gap-2 text-sm">
        <label className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="text-xs text-muted">Archetype</span>
          <select
            value={detail.archetype ?? ""}
            disabled={!templates || saving}
            onChange={(e) => save({ archetype: e.target.value })}
            className="rounded-md border border-border bg-surface-2 px-2 py-1"
          >
            <option value="">Pick an archetype…</option>
            {templates?.map((t) => (
              <option key={t.key} value={t.key}>
                {t.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex w-20 flex-col gap-1">
          <span className="text-xs text-muted">Bracket</span>
          <select
            value={detail.bracket ?? ""}
            disabled={saving}
            onChange={(e) => save({ bracket: Number(e.target.value) })}
            className="rounded-md border border-border bg-surface-2 px-2 py-1"
          >
            {detail.bracket === null && <option value="">—</option>}
            {BRACKETS.map((b) => (
              <option key={b} value={b}>
                {b}
              </option>
            ))}
          </select>
        </label>
      </div>

      {error && <p className="mt-3 text-sm text-danger">{error}</p>}

      {!detail.archetype && (
        <p className="mt-3 text-sm text-muted">
          Pick an archetype to see your deck against its target counts.
        </p>
      )}

      {detail.archetype && current && current.rows.length > 0 && (
        <>
          <FitList title="Card types (of 99)" rows={typeRows} />
          <FitList title="Roles" rows={roleRows} />
        </>
      )}
    </div>
  );
}

function FitList({ title, rows }: { title: string; rows: TemplateFit["rows"] }) {
  return (
    <>
      <h3 className="mt-4 text-xs font-semibold uppercase tracking-wide text-muted">{title}</h3>
      <ul className="mt-2 space-y-1 text-xs">
        {rows.map((r) => {
          const { value, note } = formatFitRow(r);
          const green = fitTone(r.status) === "green";
          return (
            <li
              key={r.key}
              data-status={r.status}
              className={`flex items-baseline justify-between gap-2 rounded px-2 py-1 ${
                green ? "bg-success/10 text-success" : "bg-warning/10 text-warning"
              }`}
              title={note ?? undefined}
            >
              <span className="min-w-0 text-foreground/80">{r.label}</span>
              <span className="shrink-0 text-right font-mono">
                {value}
                {note && <span className="ml-2 font-sans text-[10px] opacity-80">{note}</span>}
              </span>
            </li>
          );
        })}
      </ul>
    </>
  );
}
