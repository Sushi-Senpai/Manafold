"use client";

// useActiveCardTrigger turns any element into a trigger for the sticky
// card-image panel: it returns the pointer + focus handlers a card-name surface
// spreads onto its element. Hover and keyboard focus both drive the panel, on
// every pointer type (a coarse pointer just never fires the hover half —
// DECK-075).
//
// @spec DECK-072, DECK-074, DECK-075, DECK-076

import { useCallback, useEffect, useId } from "react";

import type { PreviewCard } from "@/lib/cardPreview";
import { useActiveCard } from "./ActiveCardContext";

export function useActiveCardTrigger(card: PreviewCard | null) {
  const panel = useActiveCard();
  const key = useId();

  const open = useCallback(() => {
    if (card) panel.set(key, card);
  }, [panel, card, key]);

  const close = useCallback(() => {
    panel.clear(key);
  }, [panel, key]);

  // If the surface unmounts while it is the one showing (e.g. a search result
  // list is replaced mid-hover), release the panel immediately. `clearNow` is a
  // stable reference, so this effect runs its cleanup only on real unmount — not
  // on every re-render — and it skips the debounced `clear`, whose shared timer
  // races when a whole list unmounts at once and leaves the panel stuck.
  const { clearNow } = panel;
  useEffect(() => () => clearNow(key), [clearNow, key]);

  return {
    open,
    close,
    handlers: {
      onMouseEnter: open,
      onMouseLeave: close,
      onFocus: open,
      onBlur: close,
    },
  };
}
