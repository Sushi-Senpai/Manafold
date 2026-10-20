import { test } from "node:test";
import assert from "node:assert/strict";

import { ensureOnce } from "./synergy.ts";

// @spec SYN-044
test("ensureOnce calls ensure once per commander and ignores no commander", () => {
  const done = new Set<string>();
  const calls: string[] = [];
  const ensure = async (id: string) => {
    calls.push(id);
  };

  ensureOnce(done, null, ensure);
  ensureOnce(done, "a", ensure);
  ensureOnce(done, "a", ensure);
  ensureOnce(done, "b", ensure);

  assert.deepEqual(calls, ["a", "b"]);
});

// @spec SYN-044
test("ensureOnce swallows a failed ensure and does not retry it", async () => {
  const done = new Set<string>();
  let calls = 0;
  const failing = async () => {
    calls++;
    throw new Error("503");
  };

  ensureOnce(done, "a", failing);
  await new Promise((r) => setTimeout(r, 0));
  ensureOnce(done, "a", failing);

  assert.equal(calls, 1);
});
