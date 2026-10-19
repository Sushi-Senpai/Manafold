// Pure view helpers for the builder's Synergy panel. Kept out of the component
// so they can be unit-tested (see synergy.test.ts).
//
// @spec SYN-043

import type { CommanderSynergy, DeckDetail } from "./api.ts";

// The attribution Archidekt's developer asked for, shown wherever synergy data
// is displayed. The backend sends the same text and link; these are the
// fallback if a response lacks them.
export const ATTRIBUTION_TEXT = "Synergy data from public Archidekt decks";
export const ATTRIBUTION_URL = "https://archidekt.com";

// percent renders a 0..1 rate as a whole percentage ("87%").
export function percent(rate: number): string {
  return `${Math.round(rate * 100)}%`;
}

// signedPercent renders a synergy score as a signed whole percentage ("+87%",
// "-12%"), or an em dash when there is no baseline to compare against. A score
// that rounds to zero is shown unsigned.
export function signedPercent(synergy: number | null): string {
  if (synergy === null) return "—";
  const n = Math.round(synergy * 100);
  if (n === 0) return "0%";
  return `${n > 0 ? "+" : "-"}${Math.abs(n)}%`;
}

// cardIdsInDeck is the set of card ids already on the main or command board,
// the cards the panel marks "In deck" instead of offering Add.
//
// @spec SYN-043
export function cardIdsInDeck(detail: DeckDetail): Set<string> {
  const ids = new Set<string>();
  for (const board of ["command", "main"] as const) {
    for (const e of detail.boards[board] ?? []) ids.add(e.card_id);
  }
  return ids;
}

// sampleFooter reads "Based on 58 public decks · fetched 2026-10-19", the line
// that tells the reader how much data the percentages rest on.
//
// @spec SYN-043
export function sampleFooter(data: CommanderSynergy): string {
  const n = data.sample.deck_count;
  const fetched = data.sample.fetched_at.slice(0, 10);
  return `Based on ${n} public deck${n === 1 ? "" : "s"} · fetched ${fetched}`;
}
