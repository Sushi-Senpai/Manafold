// Pure helpers for the builder page. Kept out of the component so they can be
// unit-tested (see deck.test.ts).
//
// @spec DECK-007, DECK-008

import type { AISuggestResponse, DeckEntry, ValidationReport } from "./api";
import type { PreviewCard } from "./cardPreview";

export const BOARD_ORDER = ["command", "main", "maybe", "sideboard"] as const;
export type BoardName = (typeof BOARD_ORDER)[number];

export const BOARD_LABELS: Record<BoardName, string> = {
  command: "Commander",
  main: "Mainboard",
  maybe: "Considering",
  sideboard: "Sideboard",
};

// ---- decklist type grouping (DECK-087, DECK-088) -----------------------

// The card types the decklist buckets entries into. TYPE_PRECEDENCE is the
// order a multi-type card is classified in (first substring match wins), chosen
// to mirror `internal/deckstats`' type precedence so the decklist sections and
// the stats type counts agree — "Artifact Creature — Golem" lands under
// Creature, "Artifact Land" under Land. TYPE_DISPLAY_ORDER is the order the
// sections are shown in, matching the Moxfield reading order the captain asked
// for. "Other" catches tribal, scheme, and anything unrecognised.
const TYPE_PRECEDENCE: { label: string; needle: string }[] = [
  { label: "Creatures", needle: "creature" },
  { label: "Planeswalkers", needle: "planeswalker" },
  { label: "Lands", needle: "land" },
  { label: "Artifacts", needle: "artifact" },
  { label: "Enchantments", needle: "enchantment" },
  { label: "Instants", needle: "instant" },
  { label: "Sorceries", needle: "sorcery" },
  { label: "Battles", needle: "battle" },
];

export const TYPE_DISPLAY_ORDER = [
  "Creatures",
  "Instants",
  "Sorceries",
  "Artifacts",
  "Enchantments",
  "Planeswalkers",
  "Battles",
  "Lands",
  "Other",
] as const;

// primaryCardType derives a decklist bucket from a Scryfall type line. It reads
// the front face of a `//` card (the face you cast) and returns the first
// TYPE_PRECEDENCE label whose needle appears in it, else "Other".
//
// @spec DECK-088
export function primaryCardType(typeLine: string): string {
  const front = typeLine.split(" // ")[0].toLowerCase();
  for (const { label, needle } of TYPE_PRECEDENCE) {
    if (front.includes(needle)) return label;
  }
  return "Other";
}

export type TypeGroup = { type: string; count: number; entries: DeckEntry[] };

// groupByType buckets a board's entries by primary card type, in
// TYPE_DISPLAY_ORDER, dropping empty buckets. `count` is the summed quantity of
// the bucket (so "Creatures (15)" counts copies, not rows); entries are sorted
// by name within a bucket.
//
// @spec DECK-087
export function groupByType(entries: DeckEntry[]): TypeGroup[] {
  const buckets = new Map<string, DeckEntry[]>();
  for (const e of entries) {
    const key = primaryCardType(e.type_line);
    const list = buckets.get(key) ?? [];
    list.push(e);
    buckets.set(key, list);
  }
  const groups: TypeGroup[] = [];
  for (const type of TYPE_DISPLAY_ORDER) {
    const list = buckets.get(type);
    if (!list || list.length === 0) continue;
    groups.push({
      type,
      count: list.reduce((n, e) => n + e.quantity, 0),
      entries: [...list].sort((x, y) => x.name.localeCompare(y.name)),
    });
  }
  return groups;
}

// ---- card-image panel active card (DECK-072) --------------------------

export type PanelCard =
  | { card: PreviewCard; source: "active" | "commander" }
  | { card: null; source: "placeholder" };

// resolvePanelCard decides what the sticky card-image panel shows: the card
// currently hovered / keyboard-focused, else the deck's commander, else a
// neutral placeholder.
//
// @spec DECK-072
export function resolvePanelCard(
  active: PreviewCard | null,
  commander: PreviewCard | null,
): PanelCard {
  if (active) return { card: active, source: "active" };
  if (commander) return { card: commander, source: "commander" };
  return { card: null, source: "placeholder" };
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
