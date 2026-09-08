"use client";

// HoverCardName wraps a card's name (or any inline card reference) so pointing
// at it raises the shared floating preview. It is the single surface every part
// of the builder uses for a hoverable card name — search results, decklist
// rows, the commander display, the commander-picker autocomplete.
//
// @spec DECK-076

import type { ReactNode } from "react";

import { useHoverPreview } from "./useHoverPreview";
import type { PreviewCard } from "./CardPreviewContext";

export function HoverCardName({
  card,
  children,
  className,
}: {
  card: PreviewCard;
  children: ReactNode;
  className?: string;
}) {
  const { handlers } = useHoverPreview(card);
  return (
    <span className={className} {...handlers}>
      {children}
    </span>
  );
}
