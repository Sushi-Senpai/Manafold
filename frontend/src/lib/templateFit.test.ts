import { test } from "node:test";
import assert from "node:assert/strict";

import type { TemplateRowFit } from "./api.ts";
import { fitTone, formatFitRow } from "./templateFit.ts";

const lands: TemplateRowFit = {
  key: "lands",
  label: "Lands",
  kind: "type",
  min: 36,
  max: 38,
  relaxed_from_bracket: 4,
  count: 37,
  status: "ok",
};

// @spec DECK-109
test("fitTone is green in range or relaxed, amber outside", () => {
  assert.equal(fitTone("ok"), "green");
  assert.equal(fitTone("relaxed"), "green");
  assert.equal(fitTone("under"), "amber");
  assert.equal(fitTone("over"), "amber");
});

test("formatFitRow shows count against the range with a status note", () => {
  assert.deepEqual(formatFitRow(lands), { value: "37 / 36–38", note: null });
  assert.deepEqual(formatFitRow({ ...lands, count: 33, status: "under" }), {
    value: "33 / 36–38",
    note: "3 under",
  });
  assert.deepEqual(formatFitRow({ ...lands, count: 40, status: "over" }), {
    value: "40 / 36–38",
    note: "2 over",
  });
  assert.equal(
    formatFitRow({ ...lands, count: 31, status: "relaxed" }).note,
    "Bracket 4+: fewer lands OK with heavy fast mana",
  );
});
