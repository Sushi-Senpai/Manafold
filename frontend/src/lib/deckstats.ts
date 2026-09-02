// Pure view helpers over the deterministic DeckStats payload. Kept out of the
// component so they can be unit-tested (see deckstats.test.ts).
//
// @spec DECK-051, DECK-052

import type { DeckStats } from "./api.ts";

export const CURVE_BUCKETS = ["0", "1", "2", "3", "4", "5", "6", "7+"] as const;
export const WUBRG = ["W", "U", "B", "R", "G"] as const;

// curveRows returns every curve column in display order, filling gaps with zero
// so the chart axis has no holes.
export function curveRows(stats: DeckStats): { bucket: string; count: number }[] {
  return CURVE_BUCKETS.map((bucket) => ({ bucket, count: stats.mana_curve[bucket] ?? 0 }));
}

// pipRows pairs each colour's pip demand with how many sources produce it, so
// the UI can show "the deck wants a lot of blue but runs few blue sources".
export function pipRows(
  stats: DeckStats,
): { color: string; pips: number; sources: number }[] {
  return WUBRG.filter(
    (c) => (stats.color_pips[c] ?? 0) > 0 || (stats.color_sources[c] ?? 0) > 0,
  ).map((color) => ({
    color,
    pips: stats.color_pips[color] ?? 0,
    sources: stats.color_sources[color] ?? 0,
  }));
}

export type CategoryRow = {
  name: string;
  count: number;
  min: number | null;
  max: number | null;
  status: "under" | "over" | "ok" | "untargeted";
};

// categoryRows merges the deck's category counts with the Commander
// rules-of-thumb targets, so every targeted category shows even at zero and
// free-text categories still appear.
export function categoryRows(stats: DeckStats): CategoryRow[] {
  const names = new Set<string>([
    ...Object.keys(stats.category_targets ?? {}),
    ...Object.keys(stats.category_counts ?? {}),
  ]);
  const rows: CategoryRow[] = [];
  for (const name of names) {
    const count = stats.category_counts[name] ?? 0;
    const target = stats.category_targets?.[name];
    let min: number | null = null;
    let max: number | null = null;
    let status: CategoryRow["status"] = "untargeted";
    if (target) {
      [min, max] = target;
      status = count < min ? "under" : count > max ? "over" : "ok";
    }
    rows.push({ name, count, min, max, status });
  }
  return rows.sort((a, b) => a.name.localeCompare(b.name));
}
