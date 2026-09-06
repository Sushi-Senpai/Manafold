// Pure helpers for the builder page. Kept out of the component so they can be
// unit-tested (see deck.test.ts).
//
// @spec DECK-007, DECK-008

import type { AISuggestResponse, DeckEntry, ValidationReport } from "./api";

export const BOARD_ORDER = ["command", "main", "maybe", "sideboard"] as const;
export type BoardName = (typeof BOARD_ORDER)[number];

export const BOARD_LABELS: Record<BoardName, string> = {
  command: "Command zone",
  main: "Mainboard",
  maybe: "Maybeboard",
  sideboard: "Sideboard",
};

// groupByCategory buckets a board's entries by their functional category,
// with uncategorised entries under "Uncategorised", each bucket name sorted.
export function groupByCategory(entries: DeckEntry[]): { category: string; entries: DeckEntry[] }[] {
  const buckets = new Map<string, DeckEntry[]>();
  for (const e of entries) {
    const key = e.category && e.category.trim() !== "" ? e.category : "Uncategorised";
    const list = buckets.get(key) ?? [];
    list.push(e);
    buckets.set(key, list);
  }
  return [...buckets.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([category, list]) => ({
      category,
      entries: [...list].sort((x, y) => x.name.localeCompare(y.name)),
    }));
}

// boardCount sums the quantities on a board.
export function boardCount(entries: DeckEntry[]): number {
  return entries.reduce((n, e) => n + e.quantity, 0);
}

// formatValidationStrip turns a validation report into the short lines the
// builder's live strip shows.
export function formatValidationStrip(report: ValidationReport): string[] {
  const lines: string[] = [];

  const count = report.main_command_count;
  lines.push(`${count}/100`);

  if (report.color_identity_violations.length > 0) {
    lines.push(
      `${report.color_identity_violations.length} card${
        report.color_identity_violations.length === 1 ? "" : "s"
      } outside colour identity`,
    );
  }
  for (const v of report.singleton_violations) {
    lines.push(`singleton: ${v.quantity}× ${v.card_name}`);
  }
  for (const v of report.banlist_violations) {
    lines.push(`banned: ${v.card_name}`);
  }
  for (const issue of report.commander_issues ?? []) {
    lines.push(issue);
  }
  if (lines.length === 1 && report.legal) {
    lines.push("legal");
  }
  return lines;
}

// formatSuggestionsFooter renders the line under the AI suggestions list: the
// model id, and — when the anti-hallucination gate rejected one or more
// model-named cards — how many it dropped. Drops are reported, never quietly
// back-filled with a substitute.
//
// @spec AI-013, AI-020
export function formatSuggestionsFooter(result: AISuggestResponse): string {
  if (result.dropped > 0) {
    return `${result.model} · ${result.dropped} dropped by the legality check`;
  }
  return result.model;
}

// explainFitLabel is the caption on a decklist entry's "Explain fit" button:
// it shows progress while a blurb is being fetched and, once one has been
// shown, invites a fresh take.
//
// @spec AI-021
export function explainFitLabel(state: { loading: boolean; hasBlurb: boolean }): string {
  if (state.loading) return "Explaining…";
  return state.hasBlurb ? "Explain fit again" : "Explain fit";
}
