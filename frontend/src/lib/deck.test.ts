import { test } from "node:test";
import assert from "node:assert/strict";

import {
  groupByType,
  primaryCardType,
  resolvePanelCard,
  boardCount,
  formatValidationStrip,
  formatSuggestionsFooter,
  explainFitLabel,
} from "./deck.ts";
import type { AISuggestResponse, DeckEntry, ValidationReport } from "./api.ts";

function entry(name: string, typeLine: string, quantity = 1): DeckEntry {
  return {
    entry_id: name,
    card_id: name,
    name,
    mana_cost: null,
    mana_value: 0,
    type_line: typeLine,
    color_identity: [],
    quantity,
    board: "main",
    category: null,
    image_uris: null,
    prices: null,
    set_code: "",
    collector_number: "",
    color_identity_violation: false,
    offending_colors: [],
    singleton_violation: false,
  };
}

// @spec DECK-088
test("primaryCardType classifies by precedence and reads the front face of a // line", () => {
  assert.equal(primaryCardType("Legendary Creature — Phyrexian Angel Horror"), "Creatures");
  assert.equal(primaryCardType("Artifact Creature — Golem"), "Creatures");
  assert.equal(primaryCardType("Legendary Planeswalker — Chandra"), "Planeswalkers");
  assert.equal(primaryCardType("Basic Land — Forest"), "Lands");
  assert.equal(primaryCardType("Artifact — Equipment"), "Artifacts");
  assert.equal(primaryCardType("Enchantment"), "Enchantments");
  assert.equal(primaryCardType("Instant"), "Instants");
  assert.equal(primaryCardType("Sorcery — Arcane"), "Sorceries");
  // Battle DFC: the front face wins.
  assert.equal(primaryCardType("Battle — Siege // Creature — Elemental"), "Battles");
  // MDFC: the castable front face, not the land back.
  assert.equal(primaryCardType("Instant // Land"), "Instants");
  assert.equal(primaryCardType("Tribal Sorcery — Elf"), "Sorceries");
  assert.equal(primaryCardType("Scheme"), "Other");
});

// @spec DECK-087
test("groupByType buckets entries in display order, counts copies, sorts by name, drops empties", () => {
  const groups = groupByType([
    entry("Wrath of God", "Sorcery"),
    entry("Sol Ring", "Artifact"),
    entry("Birds of Paradise", "Creature — Bird"),
    entry("Forest", "Basic Land — Forest", 12),
    entry("Counterspell", "Instant"),
    entry("Blasphemous Act", "Sorcery"),
    entry("Llanowar Elves", "Creature — Elf Druid"),
  ]);

  assert.deepEqual(
    groups.map((g) => [g.type, g.count]),
    [
      ["Creatures", 2],
      ["Instants", 1],
      ["Sorceries", 2],
      ["Artifacts", 1],
      ["Lands", 12],
    ],
  );
  assert.deepEqual(groups[0].entries.map((e) => e.name), ["Birds of Paradise", "Llanowar Elves"]);
  assert.deepEqual(groups[2].entries.map((e) => e.name), ["Blasphemous Act", "Wrath of God"]);
});

// @spec DECK-072
test("resolvePanelCard falls back active card -> commander -> placeholder", () => {
  const active = { name: "Sol Ring", mana_cost: "{1}", type_line: "Artifact", image_uris: null };
  const commander = {
    name: "Atraxa",
    mana_cost: "{G}{W}{U}{B}",
    type_line: "Legendary Creature",
    image_uris: null,
  };
  assert.deepEqual(resolvePanelCard(active, commander), { card: active, source: "active" });
  assert.deepEqual(resolvePanelCard(null, commander), { card: commander, source: "commander" });
  assert.deepEqual(resolvePanelCard(null, null), { card: null, source: "placeholder" });
});

test("boardCount sums quantities", () => {
  assert.equal(boardCount([entry("Mountain", "Basic Land — Mountain", 34), entry("Sol Ring", "Artifact", 1)]), 35);
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

// @spec AI-013, AI-020
test("formatSuggestionsFooter shows just the model id when the gate dropped nothing", () => {
  const result = { suggestions: [], dropped: 0, model: "claude-sonnet-5" } as AISuggestResponse;
  assert.equal(formatSuggestionsFooter(result), "claude-sonnet-5");
});

// @spec AI-013
test("formatSuggestionsFooter appends the dropped count when the gate rejected cards", () => {
  const result = { suggestions: [], dropped: 3, model: "claude-sonnet-5" } as AISuggestResponse;
  assert.equal(
    formatSuggestionsFooter(result),
    "claude-sonnet-5 · 3 dropped by the legality check",
  );
});

// @spec AI-021
test("explainFitLabel reflects the fetch state and whether a blurb is already shown", () => {
  assert.equal(explainFitLabel({ loading: false, hasBlurb: false }), "Explain fit");
  assert.equal(explainFitLabel({ loading: true, hasBlurb: false }), "Explaining…");
  assert.equal(explainFitLabel({ loading: false, hasBlurb: true }), "Explain fit again");
  assert.equal(explainFitLabel({ loading: true, hasBlurb: true }), "Explaining…");
});
