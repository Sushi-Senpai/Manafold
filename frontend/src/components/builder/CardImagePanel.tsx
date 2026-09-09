"use client";

// The sticky card-image panel: one fixed spot in the builder's left column that
// shows the card currently under the pointer / keyboard focus (any card-name
// surface), or the deck's commander at rest, or a neutral placeholder when the
// deck has no commander yet. It reserves the card aspect so the box never jumps,
// decodes a new image before swapping it in (the previous image stays until
// then), and renders the shared CardFrame for a card with no art.
//
// @spec DECK-070, DECK-071, DECK-072, DECK-073, DECK-078

import { useEffect, useState } from "react";

import { resolvePreviewImage, CARD_ASPECT, type PreviewCard } from "@/lib/cardPreview";
import { resolvePanelCard } from "@/lib/deck";
import { useActiveCard } from "./ActiveCardContext";
import { CardFrame } from "./CardFrame";

export function CardImagePanel({ commander }: { commander: PreviewCard | null }) {
  const { active } = useActiveCard();
  const panel = resolvePanelCard(active, commander);

  const targetSrc =
    panel.card != null
      ? (() => {
          const img = resolvePreviewImage(panel.card.image_uris);
          return img.kind === "image" ? img.src : null;
        })()
      : null;

  // `shownSrc` is the image currently painted; it only advances to a new src
  // once that src has decoded, so the panel never flashes blank or broken while
  // the next art loads (DECK-073). A stale `shownSrc` for a since-changed card
  // is simply not rendered (the frame / placeholder branch runs instead).
  //
  // `failedSrc` is set-only: it records the last src whose decode failed. It is
  // never cleared. `showFrame` derives the fallback-frame condition from it
  // instead — a src that later decodes advances `shownSrc` to match `targetSrc`,
  // which flips `showFrame` back off with no reset needed.
  const [shownSrc, setShownSrc] = useState<string | null>(null);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!targetSrc || targetSrc === shownSrc) return;
    let cancelled = false;
    const img = new Image();
    img.src = targetSrc;
    const done = () => !cancelled && setShownSrc(targetSrc);
    const fail = () => !cancelled && setFailedSrc(targetSrc);
    if (img.decode) img.decode().then(done, fail);
    else {
      img.onload = done;
      img.onerror = fail;
    }
    return () => {
      cancelled = true;
    };
  }, [targetSrc, shownSrc]);

  const showFrame =
    panel.card != null &&
    (targetSrc == null || (failedSrc === targetSrc && shownSrc !== targetSrc));
  const decoding = targetSrc != null && shownSrc !== targetSrc && !showFrame;

  return (
    <div className="rounded-lg border border-border bg-surface p-3">
      <div
        className="relative w-full overflow-hidden rounded-xl bg-surface-2"
        style={{ aspectRatio: String(CARD_ASPECT) }}
        aria-live="polite"
      >
        {panel.card == null ? (
          <Placeholder />
        ) : showFrame ? (
          <CardFrame card={panel.card} />
        ) : (
          <>
            {shownSrc && (
              /* eslint-disable-next-line @next/next/no-img-element */
              <img
                src={shownSrc}
                alt={panel.card.name}
                className="absolute inset-0 h-full w-full object-cover"
              />
            )}
            {decoding && (
              <div className="absolute inset-0 animate-pulse bg-surface-2" aria-hidden />
            )}
          </>
        )}
      </div>
      <p className="mt-2 flex items-center justify-between gap-2 text-xs">
        <span className="truncate font-medium text-foreground">
          {panel.card?.name ?? "Card preview"}
        </span>
        {panel.source === "commander" && (
          <span className="shrink-0 rounded bg-surface-2 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted">
            Commander
          </span>
        )}
      </p>
    </div>
  );
}

function Placeholder() {
  return (
    <div className="flex h-full w-full flex-col items-center justify-center gap-2 p-4 text-center">
      <div className="h-10 w-10 rounded-full border-2 border-dashed border-border" aria-hidden />
      <span className="text-xs text-muted">
        Hover or focus a card name to preview it here.
      </span>
    </div>
  );
}
