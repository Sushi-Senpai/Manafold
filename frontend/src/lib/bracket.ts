// Pure view helpers over the deterministic BracketReport payload. Kept out of
// the component so they can be unit-tested (see bracket.test.ts).
//
// @spec DECK-127

import type { BracketFinding, BracketReport } from "./api.ts";

export const SPELLBOOK_COMBO_URL = "https://commanderspellbook.com/combo/";

// spellbookUrl is the combo's Commander Spellbook page; the mirror stores no
// step-by-step description, so the builder links out instead.
export function spellbookUrl(comboId: string): string {
  return SPELLBOOK_COMBO_URL + encodeURIComponent(comboId);
}

// gameChangerSummary reads "2 / 3" against a limit, "2 / ∞" when the chosen
// bracket allows any number, and just the count when no bracket is chosen.
export function gameChangerSummary(report: BracketReport): string {
  const { count, limit } = report.game_changers;
  if (report.target_bracket === null) return String(count);
  return `${count} / ${limit === null ? "∞" : limit}`;
}

// headerSummary is the collapsed section's one-line reading: estimated bracket
// (marked "~" when the tag is borderline), the chosen bracket, and the Game
// Changer budget.
export function headerSummary(report: BracketReport): string {
  const est = `${report.borderline ? "~" : ""}B${report.estimated_bracket}`;
  const chosen = report.target_bracket === null ? "no target" : `target B${report.target_bracket}`;
  return `${est} · ${chosen} · GC ${gameChangerSummary(report)}`;
}

const SEVERITY_ORDER: Record<BracketFinding["severity"], number> = {
  violation: 0,
  warning: 1,
  notice: 2,
};

// sortedFindings orders findings violation → warning → notice, keeping the
// server's order within a severity.
export function sortedFindings(findings: BracketFinding[]): BracketFinding[] {
  return findings
    .map((f, i) => ({ f, i }))
    .sort((a, b) => SEVERITY_ORDER[a.f.severity] - SEVERITY_ORDER[b.f.severity] || a.i - b.i)
    .map(({ f }) => f);
}

export const SPEED_LABELS: Record<number, string> = {
  5: "instant",
  4: "very fast",
  3: "fast",
  2: "moderate",
  1: "slow",
};

export function speedLabel(speed: number): string {
  return SPEED_LABELS[speed] ?? String(speed);
}

// BRACKET_CHOICES are the selectable chosen brackets; null clears it (DECK-128).
export const BRACKET_CHOICES: { value: number | null; label: string }[] = [
  { value: null, label: "Not set" },
  { value: 1, label: "1 · Exhibition" },
  { value: 2, label: "2 · Core" },
  { value: 3, label: "3 · Upgraded" },
  { value: 4, label: "4 · Optimized" },
  { value: 5, label: "5 · cEDH" },
];
