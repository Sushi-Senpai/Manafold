import assert from "node:assert/strict";
import { test } from "node:test";

import type {
  WizardBasics,
  WizardPlan,
  WizardProposal,
  WizardProposeResponse,
} from "./api.ts";
import {
  BRACKET_OPTIONS,
  applyBasicsPlan,
  defaultAnswers,
  emptyReasonText,
  gameChangerLine,
  canAccept,
  parsePriceCap,
  picksNote,
  proposeBody,
  rejectCard,
  removeProposal,
  visibleSteps,
  wizardErrorText,
  afterAccept,
} from "./wizard.ts";
import { ApiError } from "./api.ts";

function plan(over: Partial<WizardPlan> = {}): WizardPlan {
  return {
    ready: true,
    reason: null,
    ai_enabled: true,
    bracket: 3,
    archetype: null,
    policy: { game_changer_limit: 3, game_changers_in_deck: 1, game_changers_left: 2, guidance: "g" },
    lands: { target: 37, in_deck: 0, basics_needed: 37, fixing_applicable: true },
    synergy: null,
    ...over,
  };
}

function proposal(id: string, over: Partial<WizardProposal> = {}): WizardProposal {
  return {
    card: { id, name: `Card ${id}` } as WizardProposal["card"],
    reason: "because",
    game_changer: false,
    price_usd: null,
    synergy: null,
    ...over,
  };
}

// @spec WIZ-061
test("the wizard offers Brackets 1 through 4 only", () => {
  assert.deepEqual(
    BRACKET_OPTIONS.map((b) => b.value),
    [1, 2, 3, 4],
  );
});

// @spec WIZ-031, WIZ-012
test("parsePriceCap accepts positive numbers and rejects everything else", () => {
  assert.equal(parsePriceCap("5"), 5);
  assert.equal(parsePriceCap(" 2.50 "), 2.5);
  assert.equal(parsePriceCap(""), null);
  assert.equal(parsePriceCap("0"), null);
  assert.equal(parsePriceCap("-3"), null);
  assert.equal(parsePriceCap("abc"), null);
  assert.equal(parsePriceCap("Infinity"), null);
});

// @spec WIZ-030, WIZ-031, WIZ-032, WIZ-034
test("proposeBody carries the restrictions, rejections, and only the fields a step needs", () => {
  const a = defaultAnswers();
  a.noTutors = true;
  a.maxPriceUsd = "4";
  a.notes = "  goblins  ";
  a.includeFetch = false;
  a.landTarget = 36;

  const fixing = proposeBody("fixing", a, ["r1", "r2"]);
  assert.deepEqual(fixing, {
    step: "fixing",
    include_fetch: false,
    land_target: 36,
    restrictions: { no_tutors: true, max_price_usd: 4, notes: "goblins" },
    rejected_card_ids: ["r1", "r2"],
  });

  const picks = proposeBody("picks", a, []);
  assert.equal(picks.include_fetch, undefined);
  assert.deepEqual(picks.rejected_card_ids, []);

  const bare = proposeBody("utility", defaultAnswers(), []);
  assert.deepEqual(bare.restrictions, { no_tutors: false });
  assert.equal(bare.land_target, undefined);
});

// @spec WIZ-034, WIZ-063
test("rejecting a card remembers it once; removeProposal drops it from the list", () => {
  assert.deepEqual(rejectCard(["a"], "b"), ["a", "b"]);
  assert.deepEqual(rejectCard(["a", "b"], "b"), ["a", "b"]);
  const list = [proposal("1"), proposal("2")];
  assert.deepEqual(
    removeProposal(list, "1").map((p) => p.card.id),
    ["2"],
  );
  assert.equal(list.length, 2, "the input list is not mutated");
});

// @spec WIZ-062
test("the fixing step is skipped for a deck with fewer than two colours", () => {
  assert.deepEqual(visibleSteps(plan()), ["setup", "fixing", "utility", "picks", "basics"]);
  assert.deepEqual(
    visibleSteps(plan({ lands: { target: 37, in_deck: 0, basics_needed: 37, fixing_applicable: false } })),
    ["setup", "utility", "picks", "basics"],
  );
  assert.deepEqual(visibleSteps(null), ["setup", "fixing", "utility", "picks", "basics"]);
});

// @spec WIZ-064
test("the Game Changer budget reads against the limit and gates Accept", () => {
  assert.equal(gameChangerLine({ limit: 3, in_deck: 1, left: 2 }), "Game Changers: 1 of 3");
  assert.equal(gameChangerLine({ limit: null, in_deck: 4, left: null }), "Game Changers: 4 (no limit)");
  assert.equal(gameChangerLine({ limit: 0, in_deck: 0, left: 0 }), "Game Changers: 0 of 0 — none allowed");

  const gc = proposal("g", { game_changer: true });
  assert.equal(canAccept(gc, 2), true);
  assert.equal(canAccept(gc, 0), false);
  assert.equal(canAccept(gc, null), true);
  assert.equal(canAccept(proposal("p"), 0), true, "a plain card is never gated by the budget");

  assert.equal(afterAccept(2, gc), 1);
  assert.equal(afterAccept(0, gc), 0);
  assert.equal(afterAccept(null, gc), null);
  assert.equal(afterAccept(2, proposal("p")), 2);
});

// @spec WIZ-065
test("picksNote shows the Archidekt attribution for synergy data and a popular-picks note otherwise", () => {
  const base: Omit<WizardProposeResponse, "source" | "synergy"> = {
    step: "picks",
    summary: "",
    proposals: [],
    dropped: 0,
    pool_size: 3,
    empty_reason: null,
    game_changers: { limit: 3, in_deck: 0, left: 3 },
    combos_checked: true,
    model: "m",
  };

  const synergy: WizardProposeResponse = {
    ...base,
    source: "synergy",
    synergy: {
      status: "ready",
      sample: { deck_count: 38, fetched_at: "2026-10-29T00:00:00Z" },
      attribution: { text: "Synergy data from public Archidekt decks", url: "https://archidekt.com" },
    },
  };
  assert.deepEqual(picksNote(synergy), {
    kind: "synergy",
    text: "Synergy data from public Archidekt decks",
    url: "https://archidekt.com",
    decks: 38,
  });

  const popular: WizardProposeResponse = { ...base, source: "popular", synergy: null };
  const note = picksNote(popular);
  assert.equal(note?.kind, "popular");
  assert.match(note?.text ?? "", /popular/i);

  assert.equal(picksNote({ ...popular, source: "lands" }), null);
  assert.equal(picksNote(null), null);
});

// @spec WIZ-013
test("every empty reason has a message", () => {
  for (const r of ["land_target_met", "no_fixing_needed", "deck_full", "no_candidates"] as const) {
    assert.ok(emptyReasonText(r).length > 10, r);
  }
});

// @spec WIZ-067
test("wizardErrorText names the cause in the player's terms", () => {
  assert.match(wizardErrorText(new ApiError(403, "sign in to use AI features")), /sign in/i);
  assert.match(wizardErrorText(new ApiError(429, "x")), /limit/i);
  assert.match(wizardErrorText(new ApiError(502, "x")), /try again/i);
  assert.equal(wizardErrorText(new ApiError(503, "AI features are not configured")), "AI features are not configured");
  assert.equal(wizardErrorText(new ApiError(422, "choose a bracket before using the wizard")), "choose a bracket before using the wizard");
  assert.equal(wizardErrorText(new Error("boom")), "Something went wrong with the wizard");
});

// @spec WIZ-066
test("applyBasicsPlan adds a missing basic once and sets every row's final quantity", () => {
  const plan: WizardBasics = {
    land_target: 37,
    lands_in_deck: 7,
    basics_needed: 30,
    missing: [],
    rows: [
      { card: { id: "plains" } as WizardBasics["rows"][0]["card"], quantity: 6, in_deck: 7 },
      { card: { id: "island" } as WizardBasics["rows"][0]["card"], quantity: 24, in_deck: 0 },
    ],
  };
  assert.deepEqual(applyBasicsPlan(plan), [
    { cardId: "plains", add: false, quantity: 13 },
    { cardId: "island", add: true, quantity: 24 },
  ]);
  assert.deepEqual(applyBasicsPlan({ ...plan, rows: [] }), []);
});
