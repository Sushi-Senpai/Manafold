import { test } from "node:test";
import assert from "node:assert/strict";

import { resolvePreviewImage, CARD_ASPECT } from "./cardPreview.ts";

// @spec DECK-071
test("resolvePreviewImage prefers normal, then small, then a text frame", () => {
  assert.deepEqual(
    resolvePreviewImage({ small: "s.jpg", normal: "n.jpg" }),
    { kind: "image", src: "n.jpg" },
  );
  assert.deepEqual(resolvePreviewImage({ small: "s.jpg" }), { kind: "image", src: "s.jpg" });
  assert.deepEqual(resolvePreviewImage(null), { kind: "frame" });
  assert.deepEqual(resolvePreviewImage({}), { kind: "frame" });
  assert.deepEqual(resolvePreviewImage({ normal: "   ", small: "" }), { kind: "frame" });
});

test("CARD_ASPECT is the Scryfall card ratio", () => {
  assert.ok(Math.abs(CARD_ASPECT - 0.7176) < 0.001);
});
