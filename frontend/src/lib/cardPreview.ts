// Pure geometry + image-source helpers for the builder's floating card-image
// preview. Kept framework-free so the edge-flip / viewport-clamp / no-image
// logic is unit-testable without a DOM (see cardPreview.test.ts).
//
// @spec DECK-071, DECK-072

export type AnchorRect = { top: number; left: number; width: number; height: number };
export type Size = { width: number; height: number };

export type PreviewPlacement = {
  left: number;
  top: number;
  // Which side of the anchor the preview opens toward. "left" means the preview
  // was flipped because it would have overflowed the right edge.
  side: "right" | "left";
  // True when the preview had to be shifted up from the anchor's top edge to
  // stay inside the viewport's bottom.
  shiftedUp: boolean;
};

// A Magic card image (Scryfall `normal`) is 488x680; the preview keeps that
// 0.716 aspect at a fixed on-screen size so placement can be computed before
// the image element ever measures itself.
export const PREVIEW_WIDTH = 244;
export const PREVIEW_HEIGHT = 340;

// Gap between the anchor and the preview, and the minimum breathing room kept
// between the preview and any viewport edge.
export const PREVIEW_GAP = 16;
export const PREVIEW_MARGIN = 8;

function clamp(value: number, lo: number, hi: number): number {
  return Math.min(Math.max(value, lo), hi);
}

// computePreviewPlacement positions the preview beside the anchor. It opens to
// the anchor's right by default, flips to the left when the preview would
// overflow the right edge (and the left side has room), and otherwise keeps the
// roomier side. Vertically it aligns with the anchor's top, then clamps so the
// preview never leaves the viewport — shifting up when it would overflow the
// bottom.
//
// @spec DECK-072
export function computePreviewPlacement(
  anchor: AnchorRect,
  viewport: Size,
  preview: Size = { width: PREVIEW_WIDTH, height: PREVIEW_HEIGHT },
  opts: { gap?: number; margin?: number } = {},
): PreviewPlacement {
  const gap = opts.gap ?? PREVIEW_GAP;
  const margin = opts.margin ?? PREVIEW_MARGIN;

  const spaceRight = viewport.width - (anchor.left + anchor.width);
  const spaceLeft = anchor.left;
  const needed = preview.width + gap + margin;

  let side: "right" | "left";
  let left: number;
  if (spaceRight >= needed || spaceRight >= spaceLeft) {
    side = "right";
    left = anchor.left + anchor.width + gap;
  } else {
    side = "left";
    left = anchor.left - gap - preview.width;
  }
  left = clamp(left, margin, Math.max(margin, viewport.width - preview.width - margin));

  const maxTop = viewport.height - preview.height - margin;
  const shiftedUp = anchor.top > maxTop;
  const top = clamp(anchor.top, margin, Math.max(margin, maxTop));

  return { left, top, side, shiftedUp };
}

// ---- preview image source -------------------------------------------

export type ImageUris = { small?: string; normal?: string; large?: string } | null | undefined;

export type PreviewImage =
  // A usable image URL to preload and show.
  | { kind: "image"; src: string }
  // No usable image — the caller renders a compact text card frame instead of a
  // broken <img>.
  | { kind: "frame" };

// resolvePreviewImage picks the best preview art: `normal`, then `small`. A card
// with no `image_uris`, or only blank URLs, yields a `frame` result so callers
// never mount an empty or broken image element.
//
// @spec DECK-071
export function resolvePreviewImage(imageUris: ImageUris): PreviewImage {
  const normal = imageUris?.normal?.trim();
  if (normal) return { kind: "image", src: normal };
  const small = imageUris?.small?.trim();
  if (small) return { kind: "image", src: small };
  return { kind: "frame" };
}

// ---- hover intent --------------------------------------------------

// Delay before a hover is treated as intent to preview — long enough that
// sweeping the pointer across a list does not strobe previews, short enough to
// feel immediate (Moxfield sits in this range).
//
// @spec DECK-070
export const PREVIEW_INTENT_MS = 140;
