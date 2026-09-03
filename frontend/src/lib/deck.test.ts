import { test } from "node:test";
import assert from "node:assert/strict";

import { groupByCategory, boardCount, formatValidationStrip } from "./deck.ts";
import type { DeckEntry, ValidationReport } from "./api.ts";

function entry(name: string, category: string | null, quantity = 1): DeckEntry {
  return {
    entry_id: name,
    card_id: name,
    name,
    mana_cost: null,
    mana_value: 0,
    type_line: "",
    color_identity: [],
    quantity,
    board: "main",
    category,
    image_uris: null,
    prices: null,
    set_code: "",
    collector_number: "",
    color_identity_violation: false,
    offending_colors: [],
    singleton_violation: false,
  };
}

test("groupByCategory buckets and sorts entries, folding blanks into Uncategorised", () => {
  const groups = groupByCategory([
    entry("Cultivate", "Ramp"),
    entry("Sol Ring", "Ramp"),
    entry("Swords to Plowshares", "Removal"),
    entry("Some Card", null),
    entry("Blank Card", "  "),
  ]);

  assert.deepEqual(
    groups.map((g) => g.category),
    ["Ramp", "Removal", "Uncategorised"],
  );
  assert.deepEqual(
    groups[0].entries.map((e) => e.name),
    ["Cultivate", "Sol Ring"],
  );
  assert.deepEqual(
    groups[2].entries.map((e) => e.name),
    ["Blank Card", "Some Card"],
  );
});

test("boardCount sums quantities", () => {
  assert.equal(boardCount([entry("Mountain", "Land", 34), entry("Sol Ring", "Ramp", 1)]), 35);
});

test("formatValidationStrip renders count, violations, and commander issues", () => {
  const report: ValidationReport = {
    color_identity_violations: [
      { card_id: "a", card_name: "Counterspell", offending: ["U"] },
      { card_id: "b", card_name: "Brainstorm", offending: ["U"] },
    ],
    singleton_violations: [{ card_id: "c", card_name: "Sol Ring", quantity: 2, limit: 1 }],
    banlist_violations: [{ card_id: "d", card_name: "Channel", reason: "banned in Commander" }],
    main_command_count: 97,
    count_deviation: -3,
    commander_issues: ["no commander assigned"],
    legal: false,
  };
  const lines = formatValidationStrip(report);
  assert.equal(lines[0], "97/100");
  assert.ok(lines.includes("2 cards outside colour identity"));
  assert.ok(lines.includes("singleton: 2× Sol Ring"));
  assert.ok(lines.includes("banned: Channel"));
  assert.ok(lines.includes("no commander assigned"));
});

test("formatValidationStrip tolerates a null commander_issues from the API", () => {
  // The Go backend serialises an issue-free commander configuration as
  // `"commander_issues": null`, not `[]`. The builder must still render.
  const report = {
    color_identity_violations: [],
    singleton_violations: [],
    banlist_violations: [],
    main_command_count: 3,
    count_deviation: -97,
    commander_issues: null,
    legal: false,
  } as unknown as ValidationReport;
  assert.deepEqual(formatValidationStrip(report), ["3/100"]);
});

test("formatValidationStrip says legal for a clean 100-card deck", () => {
  const report: ValidationReport = {
    color_identity_violations: [],
    singleton_violations: [],
    banlist_violations: [],
    main_command_count: 100,
    count_deviation: 0,
    commander_issues: [],
    legal: true,
  };
  assert.deepEqual(formatValidationStrip(report), ["100/100", "legal"]);
});
