import { test } from "node:test";
import assert from "node:assert/strict";

import type { CommanderSynergy, DeckDetail } from "./api.ts";
import { cardIdsInDeck, percent, sampleFooter, signedPercent } from "./synergy.ts";

// @spec SYN-043
test("percent rounds a rate to a whole percentage", () => {
  assert.equal(percent(0.874), "87%");
  assert.equal(percent(1), "100%");
  assert.equal(percent(0), "0%");
});

// @spec SYN-043
test("signedPercent signs the score and dashes a missing baseline", () => {
  assert.equal(signedPercent(0.87), "+87%");
  assert.equal(signedPercent(-0.123), "-12%");
  assert.equal(signedPercent(0.004), "0%");
  assert.equal(signedPercent(null), "—");
});

// @spec SYN-043
test("cardIdsInDeck covers the main and command boards only", () => {
  const entry = (card_id: string) => ({ card_id }) as DeckDetail["boards"]["main"][number];
  const detail = {
    boards: {
      command: [entry("cmd")],
      main: [entry("a"), entry("b")],
      maybe: [entry("maybe")],
      sideboard: [entry("side")],
    },
  } as DeckDetail;
  assert.deepEqual([...cardIdsInDeck(detail)].sort(), ["a", "b", "cmd"]);
});

// @spec SYN-043
test("sampleFooter gives the deck count and fetch date", () => {
  const data = {
    sample: { deck_count: 58, fetched_at: "2026-10-19T12:34:56Z" },
  } as CommanderSynergy;
  assert.equal(sampleFooter(data), "Based on 58 public decks · fetched 2026-10-19");
  data.sample.deck_count = 1;
  assert.match(sampleFooter(data), /^Based on 1 public deck ·/);
});
