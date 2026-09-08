// Pure image-source helper for the builder's card-image panel. Framework-free
// so the no-image fallback is unit-testable without a DOM (see
// cardPreview.test.ts). The panel's on-screen placement is plain sticky CSS in
// the layout, so no geometry lives here any more.
//
// @spec DECK-071

// A Magic card image (Scryfall `normal`) is 488x680 — a 0.716 aspect the panel
// reserves so its box does not jump when an image finishes decoding.
export const CARD_ASPECT = 488 / 680;

export type ImageUris = { small?: string; normal?: string; large?: string } | null | undefined;

// The minimal card shape the image panel and its text-frame fallback need. Both
// CardSummary and DeckEntry structurally satisfy it, so any card-name surface
// can pass its own row straight through.
export type PreviewCard = {
  name: string;
  mana_cost: string | null;
  type_line: string;
  image_uris: { small?: string; normal?: string; large?: string } | null;
};

export type PreviewImage =
  // A usable image URL to preload and show.
  | { kind: "image"; src: string }
  // No usable image — the caller renders a compact text card frame instead of a
  // broken <img>.
  | { kind: "frame" };

// resolvePreviewImage picks the best panel art: `normal`, then `small`. A card
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
