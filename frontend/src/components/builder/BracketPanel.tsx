"use client";

import { useEffect, useState } from "react";
import { api, ApiError, type BracketReport, type DeckDetail } from "@/lib/api";
import {
  BRACKET_CHOICES,
  headerSummary,
  sortedFindings,
  speedLabel,
  spellbookUrl,
} from "@/lib/bracket";

const SEVERITY_STYLE = {
  violation: "text-danger",
  warning: "text-warning",
  notice: "text-info",
} as const;

// A collapsible bracket section for the left column, beside the deck stats
// (DECK-127). The header reads the estimated bracket, the chosen bracket, and
// the Game Changer count against its limit; open, it offers the chosen-bracket
// selector, the findings, the Game Changer / mass-land-denial / extra-turn
// cards, and each combo present in the deck with a link to its Commander
// Spellbook page. Findings are advice: nothing here blocks an edit.
//
// @spec DECK-127, DECK-128
export function BracketPanel({
  deckId,
  detail,
  onChange,
}: {
  deckId: string;
  detail: DeckDetail;
  onChange: () => void;
}) {
  const [report, setReport] = useState<BracketReport | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  // Refetch when the cards, the commander(s), or the chosen bracket change.
  const signature = JSON.stringify({
    c: detail.commander?.id ?? null,
    p: detail.partner?.id ?? null,
    t: detail.bracket,
    b: Object.fromEntries(
      Object.entries(detail.boards).map(([k, v]) => [
        k,
        v.map((e) => `${e.card_id}:${e.quantity}`).join(","),
      ]),
    ),
  });

  useEffect(() => {
    let cancelled = false;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setError(null);
    api
      .getDeckBracket(deckId)
      .then((r) => {
        if (!cancelled) setReport(r);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof ApiError ? e.message : "Failed to load bracket check");
      });
    return () => {
      cancelled = true;
    };
  }, [deckId, signature]);

  async function choose(value: number | null) {
    setSaving(true);
    setError(null);
    try {
      await api.updateDeck(deckId, { bracket: value });
      onChange();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Failed to set bracket");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-lg border border-border bg-surface">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left"
      >
        <span className="text-sm font-semibold uppercase tracking-wide text-muted">Bracket</span>
        <span className="flex items-center gap-2 text-xs text-muted">
          {report && <span className="font-mono">{headerSummary(report)}</span>}
          <span aria-hidden>{open ? "▾" : "▸"}</span>
        </span>
      </button>

      {open && (
        <div className="border-t border-border px-4 pb-4 pt-3 text-xs">
          {error && <p className="mb-2 text-sm text-danger">{error}</p>}
          {!report && !error && <p className="text-sm text-muted">Loading bracket check…</p>}

          <label className="flex items-center justify-between gap-2">
            <span className="font-semibold uppercase tracking-wide text-muted">Target bracket</span>
            <select
              value={detail.bracket ?? ""}
              disabled={saving}
              onChange={(e) => choose(e.target.value === "" ? null : Number(e.target.value))}
              className="rounded border border-border bg-background px-2 py-1"
            >
              {BRACKET_CHOICES.map((c) => (
                <option key={c.label} value={c.value ?? ""}>
                  {c.label}
                </option>
              ))}
            </select>
          </label>

          {report && (
            <>
              <p className="mt-3 text-muted">
                Estimated Bracket {report.estimated_bracket}
                {report.borderline && " (borderline)"} — {report.tag_label}
              </p>

              {!report.combo_data_available && (
                <p className="mt-2 text-warning">
                  Combo data has not been loaded yet, so combos are not checked.
                </p>
              )}

              {report.findings.length > 0 && (
                <ul className="mt-3 space-y-1">
                  {sortedFindings(report.findings).map((f, i) => (
                    <li key={i} className={SEVERITY_STYLE[f.severity]}>
                      <span className="font-semibold capitalize">{f.severity}:</span> {f.message}
                    </li>
                  ))}
                </ul>
              )}
              {report.target_bracket !== null && report.findings.length === 0 && (
                <p className="mt-3 text-success">Nothing flagged for Bracket {report.target_bracket}.</p>
              )}

              <CardList title="Game Changers" cards={report.game_changers.cards} />
              <CardList title="Mass land denial" cards={report.mass_land_denial} />
              <CardList title="Extra turns" cards={report.extra_turns} />

              {report.combos.length > 0 && (
                <>
                  <h3 className="mt-4 font-semibold uppercase tracking-wide text-muted">Combos</h3>
                  <ul className="mt-2 space-y-2">
                    {report.combos.map((c) => (
                      <li key={c.id} className="rounded border border-border px-2 py-1.5">
                        <a
                          href={spellbookUrl(c.id)}
                          target="_blank"
                          rel="noreferrer"
                          className="font-medium text-primary hover:underline"
                        >
                          {c.cards.join(" + ")}
                        </a>
                        {c.produces.length > 0 && (
                          <div className="text-muted">{c.produces.join("; ")}</div>
                        )}
                        <div className="text-muted">
                          {speedLabel(c.speed)} · {c.tag_label}
                          {" · fits Bracket "}
                          {c.min_bracket}+
                        </div>
                      </li>
                    ))}
                  </ul>
                </>
              )}

              <p className="mt-4 text-muted">
                Combo data from{" "}
                <a
                  href="https://commanderspellbook.com"
                  target="_blank"
                  rel="noreferrer"
                  className="underline"
                >
                  Commander Spellbook
                </a>
                .
              </p>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function CardList({ title, cards }: { title: string; cards: string[] }) {
  if (cards.length === 0) return null;
  return (
    <>
      <h3 className="mt-4 font-semibold uppercase tracking-wide text-muted">
        {title} ({cards.length})
      </h3>
      <p className="mt-1">{cards.join(", ")}</p>
    </>
  );
}
