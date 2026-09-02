import { test } from "node:test";
import assert from "node:assert/strict";

import type { DeckStats } from "./api.ts";
import { curveRows, pipRows, categoryRows } from "./deckstats.ts";

const base: DeckStats = {
  type_counts: { Creature: 30, Land: 37 },
  avg_mana_value: 3.1,
  mana_curve: { "1": 4, "2": 10, "3": 8, "7+": 2 },
  color_pips: { U: 20, G: 6 },
  color_sources: { U: 14, G: 10 },
  category_counts: { Ramp: 9, "Group Hug": 2 },
  land_count: 37,
  nonland_count: 62,
  category_targets: {
    Land: [36, 38],
    Ramp: [8, 12],
    "Card Draw": [8, 12],
  },
};

test("curveRows fills every bucket in order, zero for gaps", () => {
  const rows = curveRows(base);
  assert.deepEqual(
    rows.map((r) => r.bucket),
    ["0", "1", "2", "3", "4", "5", "6", "7+"],
  );
  assert.equal(rows[0].count, 0);
  assert.equal(rows[2].count, 10);
  assert.equal(rows[7].count, 2);
});

test("pipRows pairs demand with sources and hides untouched colours", () => {
  const rows = pipRows(base);
  assert.deepEqual(
    rows.map((r) => r.color),
    ["U", "G"],
  );
  assert.deepEqual(rows[0], { color: "U", pips: 20, sources: 14 });
});

test("categoryRows merges targets with counts and flags under/over/ok", () => {
  const rows = categoryRows(base);
  const byName = Object.fromEntries(rows.map((r) => [r.name, r]));

  assert.equal(byName["Card Draw"].count, 0);
  assert.equal(byName["Card Draw"].status, "under");
  assert.equal(byName["Ramp"].status, "ok");
  assert.equal(byName["Land"].status, "under"); // 0 counted, target 36-38
  assert.equal(byName["Group Hug"].status, "untargeted");
  assert.deepEqual(
    rows.map((r) => r.name),
    ["Card Draw", "Group Hug", "Land", "Ramp"],
  );
});
