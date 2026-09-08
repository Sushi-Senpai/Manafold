import { test } from "node:test";
import assert from "node:assert/strict";

import { computeMenuPlacement, viewport, MENU_GAP, MENU_MARGIN } from "./menuPlacement.ts";

const VIEWPORT = { width: 1280, height: 800 };
const MENU = { width: 208, height: 220 };

// @spec DECK-094
test("computeMenuPlacement opens below the anchor and never overlaps it", () => {
  const anchor = { top: 200, left: 900, width: 24, height: 20 };
  const p = computeMenuPlacement(anchor, MENU, VIEWPORT);
  assert.equal(p.placement, "below");
  assert.equal(p.top, anchor.top + anchor.height + MENU_GAP);
  assert.ok(p.top >= anchor.top + anchor.height, "menu top is past the anchor's bottom edge");
});

// @spec DECK-094
test("computeMenuPlacement right-aligns the menu to the anchor", () => {
  const anchor = { top: 200, left: 900, width: 24, height: 20 };
  const p = computeMenuPlacement(anchor, MENU, VIEWPORT);
  assert.equal(p.left, anchor.left + anchor.width - MENU.width);
});

// @spec DECK-094
test("computeMenuPlacement flips above when the anchor is near the viewport bottom", () => {
  const anchor = { top: 760, left: 900, width: 24, height: 20 };
  const p = computeMenuPlacement(anchor, MENU, VIEWPORT);
  assert.equal(p.placement, "above");
  assert.equal(p.top, anchor.top - MENU_GAP - MENU.height);
  assert.ok(p.top >= MENU_MARGIN);
});

// @spec DECK-094
test("computeMenuPlacement clamps into the viewport horizontally near the left edge", () => {
  const anchor = { top: 200, left: 4, width: 24, height: 20 };
  const p = computeMenuPlacement(anchor, MENU, VIEWPORT);
  assert.ok(p.left >= MENU_MARGIN, `left ${p.left} below margin`);
});

// @spec DECK-094
test("viewport falls back to a desktop size when there is no window (SSR / node)", () => {
  assert.equal(typeof window, "undefined", "precondition: node test env has no window");
  assert.deepEqual(viewport(), { width: 1280, height: 800 });
});

// @spec DECK-094
test("computeMenuPlacement defaults its viewport to the shared helper when omitted", () => {
  const anchor = { top: 200, left: 900, width: 24, height: 20 };
  assert.deepEqual(
    computeMenuPlacement(anchor, MENU),
    computeMenuPlacement(anchor, MENU, viewport()),
  );
});

// @spec DECK-094
test("computeMenuPlacement clamps into the viewport when the menu is taller than the space either way", () => {
  const tall = { width: 208, height: 780 };
  const anchor = { top: 400, left: 900, width: 24, height: 20 };
  const p = computeMenuPlacement(anchor, tall, VIEWPORT);
  assert.ok(p.top >= MENU_MARGIN, `top ${p.top} below margin`);
  assert.ok(
    p.top + tall.height <= VIEWPORT.height - MENU_MARGIN + 0.001,
    `bottom ${p.top + tall.height} past viewport`,
  );
});
