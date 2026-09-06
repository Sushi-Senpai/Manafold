import { test } from "node:test";
import assert from "node:assert/strict";

import {
  computePreviewPlacement,
  resolvePreviewImage,
  PREVIEW_WIDTH,
  PREVIEW_HEIGHT,
  PREVIEW_GAP,
  PREVIEW_MARGIN,
} from "./cardPreview.ts";

const VIEWPORT = { width: 1280, height: 800 };

// @spec DECK-072
test("computePreviewPlacement opens to the right of the anchor when there is room", () => {
  const anchor = { top: 200, left: 100, width: 120, height: 20 };
  const p = computePreviewPlacement(anchor, VIEWPORT);
  assert.equal(p.side, "right");
  assert.equal(p.left, anchor.left + anchor.width + PREVIEW_GAP);
  assert.equal(p.shiftedUp, false);
});

// @spec DECK-072
test("computePreviewPlacement flips to the left near the right edge", () => {
  const anchor = { top: 200, left: 1180, width: 80, height: 20 };
  const p = computePreviewPlacement(anchor, VIEWPORT);
  assert.equal(p.side, "left");
  assert.equal(p.left, anchor.left - PREVIEW_GAP - PREVIEW_WIDTH);
  assert.ok(p.left >= PREVIEW_MARGIN);
});

// @spec DECK-072
test("computePreviewPlacement clamps into the viewport when neither side fits", () => {
  const narrow = { width: 360, height: 640 };
  const anchor = { top: 100, left: 130, width: 100, height: 20 };
  const p = computePreviewPlacement(anchor, narrow);
  assert.ok(p.left >= PREVIEW_MARGIN, `left ${p.left} below margin`);
  assert.ok(
    p.left + PREVIEW_WIDTH <= narrow.width - PREVIEW_MARGIN,
    `right edge ${p.left + PREVIEW_WIDTH} past viewport`,
  );
});

// @spec DECK-072
test("computePreviewPlacement shifts up when the anchor is near the bottom", () => {
  const anchor = { top: 780, left: 100, width: 120, height: 20 };
  const p = computePreviewPlacement(anchor, VIEWPORT);
  assert.equal(p.shiftedUp, true);
  assert.equal(p.top, VIEWPORT.height - PREVIEW_HEIGHT - PREVIEW_MARGIN);
  assert.ok(p.top >= PREVIEW_MARGIN);
});

// @spec DECK-072
test("computePreviewPlacement never positions above the viewport top", () => {
  const anchor = { top: -40, left: 100, width: 120, height: 20 };
  const p = computePreviewPlacement(anchor, VIEWPORT);
  assert.equal(p.top, PREVIEW_MARGIN);
});

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
