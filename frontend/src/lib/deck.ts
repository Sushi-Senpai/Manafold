// Pure helpers for the builder page. Kept out of the component so they can be
// unit-tested (see deck.test.ts).
//
// @spec DECK-007, DECK-008

import type { DeckEntry, ValidationReport } from "./api";

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
