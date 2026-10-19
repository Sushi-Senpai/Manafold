// Pure view helpers over the template-fit payload. The ranges and statuses are
// computed server-side from the one authoritative template definition
// (internal/decktemplates); these only decide how a row reads.
//
// @spec DECK-109

import type { FitStatus, TemplateRowFit } from "./api.ts";

export type FitTone = "green" | "amber";

// fitTone: in range (or under the land floor a Bracket 4+ deck may go below)
// reads green; anything outside the range reads amber.
export function fitTone(status: FitStatus): FitTone {
  return status === "ok" || status === "relaxed" ? "green" : "amber";
}

// formatFitRow renders "count / min–max", plus a short note on a row whose
// status needs explaining.
export function formatFitRow(row: TemplateRowFit): { value: string; note: string | null } {
  const value = `${row.count} / ${row.min}–${row.max}`;
  let note: string | null = null;
  if (row.status === "relaxed") {
    note = `Bracket ${row.relaxed_from_bracket ?? 4}+: fewer lands OK with heavy fast mana`;
  } else if (row.status === "under") {
    note = `${row.min - row.count} under`;
  } else if (row.status === "over") {
    note = `${row.count - row.max} over`;
  }
  return { value, note };
}
