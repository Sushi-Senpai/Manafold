// Pure geometry for the decklist row's action menu. Kept framework-free so the
// flip / clamp discipline is unit-testable without a DOM (see
// menuPlacement.test.ts). The menu renders `position: fixed`, so these are
// viewport coordinates.
//
// @spec DECK-094

export type Rect = { top: number; left: number; width: number; height: number };
export type Size = { width: number; height: number };

export type MenuPlacement = {
  left: number;
  top: number;
  // "below" is the default (menu hangs under the anchor); "above" means it was
  // flipped up because it would otherwise overflow the viewport bottom.
  placement: "below" | "above";
};

// Gap between the anchor row and the menu, and the minimum breathing room kept
// between the menu and any viewport edge.
export const MENU_GAP = 4;
export const MENU_MARGIN = 8;

function clamp(value: number, lo: number, hi: number): number {
  return Math.min(Math.max(value, lo), hi);
}

// computeMenuPlacement positions the menu against the anchor (the row's `⋯`
// button). It opens directly below the anchor, right-aligned to it; flips to sit
// directly above when it would overflow the viewport bottom and there is more
// room above; and clamps horizontally so it never leaves the viewport. It never
// overlaps the anchor row itself — below/above start past the anchor edge plus
// the gap.
//
// @spec DECK-094
export function computeMenuPlacement(
  anchor: Rect,
  menu: Size,
  viewport: Size,
  opts: { gap?: number; margin?: number } = {},
): MenuPlacement {
  const gap = opts.gap ?? MENU_GAP;
  const margin = opts.margin ?? MENU_MARGIN;

  const below = anchor.top + anchor.height + gap;
  const above = anchor.top - gap - menu.height;
  const spaceBelow = viewport.height - below;
  const spaceAbove = anchor.top - margin;

  let placement: "below" | "above";
  let top: number;
  if (spaceBelow >= menu.height + margin || spaceBelow >= spaceAbove) {
    placement = "below";
    top = below;
  } else {
    placement = "above";
    top = above;
  }
  top = clamp(top, margin, Math.max(margin, viewport.height - menu.height - margin));

  // Right-align the menu to the anchor, then clamp into the viewport.
  const right = anchor.left + anchor.width;
  let left = right - menu.width;
  left = clamp(left, margin, Math.max(margin, viewport.width - menu.width - margin));

  return { left, top, placement };
}
