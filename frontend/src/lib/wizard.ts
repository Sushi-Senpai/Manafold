// Pure logic for the deck wizard panel: step order and skipping, request bodies,
// the rejected set, the Game Changer budget, messages, and the basics apply
// plan. Kept out of the component so it can be unit-tested (see wizard.test.ts).
//
// The wizard proposes; the player disposes. Nothing here adds a card — the panel
// calls the ordinary add-card endpoint only on a click.
//
// @spec WIZ-060, WIZ-061, WIZ-062, WIZ-063, WIZ-064, WIZ-065, WIZ-066, WIZ-067, WIZ-092, WIZ-105

import { ApiError } from "./api.ts";
import type {
  WizardBasics,
  WizardEmptyReason,
  WizardGameChangers,
  WizardPlan,
  WizardProposal,
  WizardProposeBody,
  WizardProposeResponse,
  WizardRestrictions,
  WizardSavedState,
  WizardStep,
} from "./api.ts";

export type WizardStepId = "setup" | "fixing" | "utility" | "picks" | "basics";

export const STEP_LABELS: Record<WizardStepId, string> = {
  setup: "Setup",
  fixing: "Fixing lands",
  utility: "Utility lands",
  picks: "Picks",
  basics: "Basics",
};

// BRACKET_OPTIONS are the brackets the wizard supports; Bracket 5 is out of
// scope (WIZ-061).
export const BRACKET_OPTIONS: { value: number; label: string }[] = [
  { value: 1, label: "1 · Exhibition" },
  { value: 2, label: "2 · Core" },
  { value: 3, label: "3 · Upgraded" },
  { value: 4, label: "4 · Optimized" },
];

// The player's answers, held in the panel and saved per deck (WIZ-092). The deck
// still holds the commander, bracket, archetype and every accepted card.
export type WizardAnswers = {
  noTutors: boolean;
  maxPriceUsd: string; // the raw input; "" = no cap
  notes: string;
  fixing: "include" | "basics";
  includeFetch: boolean;
  utility: boolean;
  landTarget: number | null; // null = the plan's default
};

export function defaultAnswers(): WizardAnswers {
  return {
    noTutors: false,
    maxPriceUsd: "",
    notes: "",
    fixing: "include",
    includeFetch: true,
    utility: true,
    landTarget: null,
  };
}

// visibleSteps is the order the panel walks. A deck with fewer than two colours
// has nothing to fix, so the fixing step is left out (WIZ-013, WIZ-062).
export function visibleSteps(plan: WizardPlan | null): WizardStepId[] {
  const all: WizardStepId[] = ["setup", "fixing", "utility", "picks", "basics"];
  if (plan && !plan.lands.fixing_applicable) return all.filter((s) => s !== "fixing");
  return all;
}

// toSavedState is what the panel saves: the answers, the rejected set and the
// step the player is on (WIZ-092). Fields are always present so a save replaces
// the stored state whole.
export function toSavedState(step: WizardStepId, a: WizardAnswers, rejected: string[]): WizardSavedState {
  const notes = a.notes.trim();
  const cap = parsePriceCap(a.maxPriceUsd);
  return {
    step,
    restrictions: {
      no_tutors: a.noTutors,
      ...(cap !== null ? { max_price_usd: cap } : {}),
      ...(notes !== "" ? { notes } : {}),
    },
    fixing: a.fixing,
    include_fetch: a.includeFetch,
    utility: a.utility,
    land_target: a.landTarget,
    rejected_card_ids: rejected,
  };
}

// fromSavedState turns a saved state back into the panel's answers and rejected
// set; the step is resumed separately, once the plan says what can be shown.
export function fromSavedState(s: WizardSavedState): { answers: WizardAnswers; rejected: string[] } {
  return {
    answers: {
      noTutors: s.restrictions.no_tutors ?? false,
      maxPriceUsd: s.restrictions.max_price_usd !== undefined ? String(s.restrictions.max_price_usd) : "",
      notes: s.restrictions.notes ?? "",
      fixing: s.fixing,
      includeFetch: s.include_fetch,
      utility: s.utility,
      landTarget: s.land_target,
    },
    rejected: s.rejected_card_ids,
  };
}

// resumeStep is the step to reopen at: the saved one when the deck can show it,
// else setup (WIZ-105). Past setup the deck needs a commander and a supported
// bracket (the plan is ready), and the fixing step needs two or more colours.
export function resumeStep(saved: WizardStepId, plan: WizardPlan | null): WizardStepId {
  if (saved === "setup" || !plan || !plan.ready) return "setup";
  return visibleSteps(plan).includes(saved) ? saved : "setup";
}

// parsePriceCap reads the price input: a positive finite number, else null.
export function parsePriceCap(input: string): number | null {
  const n = Number(input.trim());
  return input.trim() !== "" && Number.isFinite(n) && n > 0 ? n : null;
}

function restrictionsOf(a: WizardAnswers): WizardRestrictions {
  const r: WizardRestrictions = { no_tutors: a.noTutors };
  const cap = parsePriceCap(a.maxPriceUsd);
  if (cap !== null) r.max_price_usd = cap;
  const notes = a.notes.trim();
  if (notes !== "") r.notes = notes;
  return r;
}

// proposeBody is the request for one round: the restrictions and the cards the
// player has rejected so far (WIZ-030..034).
export function proposeBody(step: WizardStep, a: WizardAnswers, rejected: string[]): WizardProposeBody {
  const body: WizardProposeBody = { step, restrictions: restrictionsOf(a), rejected_card_ids: rejected };
  if (step === "fixing") body.include_fetch = a.includeFetch;
  if (step !== "picks" && a.landTarget !== null) body.land_target = a.landTarget;
  return body;
}

// rejectCard remembers a rejected card once (WIZ-063).
export function rejectCard(rejected: string[], cardId: string): string[] {
  return rejected.includes(cardId) ? rejected : [...rejected, cardId];
}

export function removeProposal(list: WizardProposal[], cardId: string): WizardProposal[] {
  return list.filter((p) => p.card.id !== cardId);
}

// gameChangerLine reads the budget against the bracket's limit (WIZ-064).
export function gameChangerLine(gc: WizardGameChangers): string {
  if (gc.limit === null) return `Game Changers: ${gc.in_deck} (no limit)`;
  const base = `Game Changers: ${gc.in_deck} of ${gc.limit}`;
  return gc.limit === 0 ? `${base} — none allowed` : base;
}

// canAccept: a Game Changer cannot be accepted once the budget is spent. The
// server never offers one then; this guards a budget spent by hand meanwhile.
export function canAccept(p: WizardProposal, gcLeft: number | null): boolean {
  return !(p.game_changer && gcLeft !== null && gcLeft <= 0);
}

// afterAccept is the budget left once the player accepts the proposal.
export function afterAccept(gcLeft: number | null, p: WizardProposal): number | null {
  return p.game_changer && gcLeft !== null ? Math.max(0, gcLeft - 1) : gcLeft;
}

export type PicksNote =
  | { kind: "synergy"; text: string; url: string; decks: number }
  | { kind: "popular"; text: string };

// picksNote is the line shown under a picks round: the Archidekt attribution
// with the sample size when the numbers are synergy data (SYN-043), else a note
// that popular-card picks are standing in (WIZ-065).
export function picksNote(resp: WizardProposeResponse | null): PicksNote | null {
  if (!resp) return null;
  if (resp.source === "synergy" && resp.synergy) {
    return {
      kind: "synergy",
      text: resp.synergy.attribution.text,
      url: resp.synergy.attribution.url,
      decks: resp.synergy.sample?.deck_count ?? 0,
    };
  }
  if (resp.source === "popular") {
    return {
      kind: "popular",
      text: "Showing popular-card picks: commander-specific synergy data is not available yet (it may still be gathering).",
    };
  }
  return null;
}

const EMPTY_TEXT: Record<WizardEmptyReason, string> = {
  land_target_met: "The deck already has as many lands as its target, so there is nothing more to add here.",
  no_fixing_needed: "A deck with fewer than two colours has nothing to fix.",
  deck_full: "The deck already has 100 cards.",
  no_candidates: "Nothing in the pool fits your rules. Try loosening the price cap or the tutor ban.",
};

export function emptyReasonText(reason: WizardEmptyReason): string {
  return EMPTY_TEXT[reason];
}

// wizardErrorText names why a wizard request failed in the player's terms
// (WIZ-067).
export function wizardErrorText(e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.status) {
      case 403:
        return "Sign in to use the wizard's AI picks. Setup and the basics step work without it.";
      case 429:
        return "You have reached today's wizard limit. Try again tomorrow.";
      case 502:
        return "The model did not return usable picks. Try again in a moment.";
      default:
        return e.message;
    }
  }
  return "Something went wrong with the wizard";
}

export type BasicsAction = { cardId: string; add: boolean; quantity: number };

// applyBasicsPlan turns the server's plan into the calls the panel makes: add a
// basic that is not on the deck yet, then set every row to its final quantity
// (WIZ-066). The add endpoint only ever adds one copy.
export function applyBasicsPlan(plan: WizardBasics): BasicsAction[] {
  return plan.rows.map((r) => ({
    cardId: r.card.id,
    add: r.in_deck === 0,
    quantity: r.in_deck + r.quantity,
  }));
}
