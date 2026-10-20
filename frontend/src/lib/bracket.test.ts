import assert from "node:assert/strict";
import { test } from "node:test";

import type { BracketFinding, BracketReport } from "./api.ts";
import {
  gameChangerSummary,
  headerSummary,
  sortedFindings,
  spellbookUrl,
  speedLabel,
} from "./bracket.ts";

function report(over: Partial<BracketReport> = {}): BracketReport {
  return {
    tag: "P",
    tag_label: "Powerful",
    estimated_bracket: 3,
    borderline: false,
    target_bracket: 3,
    game_changers: { count: 2, limit: 3, cards: [] },
    mass_land_denial: [],
    extra_turns: [],
    combos: [],
    findings: [],
    combo_data_available: true,
    ...over,
  };
}

// @spec DECK-127
test("gameChangerSummary shows the count against the limit", () => {
  assert.equal(gameChangerSummary(report()), "2 / 3");
  assert.equal(
    gameChangerSummary(report({ target_bracket: 4, game_changers: { count: 5, limit: null, cards: [] } })),
    "5 / ∞",
  );
  assert.equal(
    gameChangerSummary(report({ target_bracket: null, game_changers: { count: 1, limit: null, cards: [] } })),
    "1",
  );
});

// @spec DECK-127
test("headerSummary gives the estimate, the chosen bracket, and the Game Changers", () => {
  assert.equal(headerSummary(report()), "B3 · target B3 · GC 2 / 3");
  assert.equal(
    headerSummary(report({ borderline: true, target_bracket: null })),
    "~B3 · no target · GC 2",
  );
});

// @spec DECK-127
test("sortedFindings puts violations first and keeps the server order within a severity", () => {
  const f = (severity: BracketFinding["severity"], message: string): BracketFinding => ({
    severity,
    kind: "combo",
    message,
  });
  const out = sortedFindings([f("notice", "n"), f("warning", "w1"), f("violation", "v"), f("warning", "w2")]);
  assert.deepEqual(
    out.map((x) => x.message),
    ["v", "w1", "w2", "n"],
  );
});

// @spec DECK-127
test("spellbookUrl links to the combo page and speedLabel names the speed", () => {
  assert.equal(spellbookUrl("742-1295"), "https://commanderspellbook.com/combo/742-1295");
  assert.equal(speedLabel(5), "instant");
  assert.equal(speedLabel(1), "slow");
  assert.equal(speedLabel(9), "9");
});
