"use client";

// useHoverPreview turns any element into a card-preview trigger: it returns the
// pointer / focus handlers a card-name surface spreads onto its element. A hover
// only raises the preview after a short intent delay, and only on hover-capable
// pointers; every trigger shares one preview via CardPreviewContext.
//
// @spec DECK-070, DECK-074, DECK-075

import { useCallback, useEffect, useId, useRef } from "react";

import { PREVIEW_INTENT_MS } from "@/lib/cardPreview";
import { useCardPreview, type PreviewCard } from "./CardPreviewContext";

export function useHoverPreview(card: PreviewCard | null) {
  const preview = useCardPreview();
  const key = useId();
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancelTimer = useCallback(() => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
  }, []);

  const open = useCallback(
    (anchorEl: Element) => {
      if (!preview.hoverable || !card) return;
      cancelTimer();
      timer.current = setTimeout(() => preview.show(key, card, anchorEl), PREVIEW_INTENT_MS);
    },
    [preview, card, key, cancelTimer],
  );

  const close = useCallback(() => {
    cancelTimer();
    preview.hide(key);
  }, [preview, key, cancelTimer]);

  useEffect(() => cancelTimer, [cancelTimer]);

  const disabled = !preview.hoverable || !card;

  return {
    disabled,
    // `open` / `close` are also exposed directly so a list can drive the preview
    // from its keyboard cursor, not only from the pointer.
    open,
    close,
    handlers: disabled
      ? {}
      : {
          onMouseEnter: (e: { currentTarget: Element }) => open(e.currentTarget),
          onMouseLeave: close,
        },
  };
}
