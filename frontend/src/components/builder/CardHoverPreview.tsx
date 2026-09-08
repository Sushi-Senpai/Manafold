"use client";

// The floating card image itself: a fixed-position element portalled to
// <body> (re-scoped into `.workspace` so it picks up the builder's light
// palette). It computes its own on-screen placement from the anchor rect,
// flips / clamps at viewport edges, preloads and decodes the art before
// revealing it, and falls back to a text card frame when the card has no image.
//
// @spec DECK-071, DECK-072, DECK-073

import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";

import {
  computePreviewPlacement,
  resolvePreviewImage,
  PREVIEW_WIDTH,
  PREVIEW_HEIGHT,
  type AnchorRect,
} from "@/lib/cardPreview";
import type { PreviewCard } from "./CardPreviewContext";

function viewport() {
  if (typeof window === "undefined") return { width: 1280, height: 800 };
  return { width: window.innerWidth, height: window.innerHeight };
}

export function CardHoverPreview({ card, anchor }: { card: PreviewCard; anchor: AnchorRect }) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-shot portal mount guard
    setMounted(true);
  }, []);

  const placement = useMemo(() => computePreviewPlacement(anchor, viewport()), [anchor]);
  const image = resolvePreviewImage(card.image_uris);
  const imgSrc = image.kind === "image" ? image.src : null;

  // Track which src has finished decoding / failed, rather than resetting a
  // boolean each time the src changes — keeps state derivable and lint-clean.
  const [decodedSrc, setDecodedSrc] = useState<string | null>(null);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!imgSrc) return;
    let cancelled = false;
    const img = new Image();
    img.src = imgSrc;
    const done = () => !cancelled && setDecodedSrc(imgSrc);
    const fail = () => !cancelled && setFailedSrc(imgSrc);
    if (img.decode) img.decode().then(done, fail);
    else {
      img.onload = done;
      img.onerror = fail;
    }
    return () => {
      cancelled = true;
    };
  }, [imgSrc]);

  if (!mounted) return null;

  const decoded = imgSrc != null && decodedSrc === imgSrc;
  const showFrame = imgSrc == null || failedSrc === imgSrc;

  const body = (
    <div
      className="workspace pointer-events-none fixed z-[60]"
      style={{ left: placement.left, top: placement.top, width: PREVIEW_WIDTH }}
      role="tooltip"
      aria-label={`${card.name} card preview`}
    >
      <div
        className="relative overflow-hidden rounded-xl border border-border bg-surface shadow-2xl ring-1 ring-black/5"
        style={{ height: PREVIEW_HEIGHT }}
      >
        {showFrame ? (
          <TextCardFrame card={card} />
        ) : (
          <>
            {!decoded && <div className="absolute inset-0 animate-pulse bg-surface-2" aria-hidden />}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={imgSrc}
              alt={card.name}
              width={PREVIEW_WIDTH}
              height={PREVIEW_HEIGHT}
              className={`absolute inset-0 h-full w-full object-cover transition-opacity duration-100 ${
                decoded ? "opacity-100" : "opacity-0"
              }`}
            />
          </>
        )}
      </div>
    </div>
  );

  return createPortal(body, document.body);
}

// TextCardFrame stands in for cards the mirror has no printing image for, so a
// hover still shows something identifying instead of a broken rectangle.
function TextCardFrame({ card }: { card: PreviewCard }) {
  return (
    <div className="flex h-full w-full flex-col gap-2 p-4">
      <div className="flex items-start justify-between gap-2">
        <span className="text-sm font-semibold leading-tight text-foreground">{card.name}</span>
        {card.mana_cost && (
          <span className="shrink-0 font-mono text-xs text-muted">{card.mana_cost}</span>
        )}
      </div>
      <span className="text-xs text-muted">{card.type_line}</span>
      <div className="mt-auto text-[10px] uppercase tracking-wide text-muted/70">No card image</div>
    </div>
  );
}
