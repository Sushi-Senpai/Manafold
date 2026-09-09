"use client";

// HoverCardName wraps a card's name (or any inline card reference) so hovering
// or keyboard-focusing it drives the sticky card-image panel. It is the single
// surface every part of the builder uses for a previewable card name — search
// results, decklist rows, the commander display, the commander-picker
// autocomplete, AI-suggestion rows.
//
// @spec DECK-076

import type { ReactNode } from "react";

import type { PreviewCard } from "@/lib/cardPreview";
import { useActiveCardTrigger } from "./useActiveCardTrigger";

export function HoverCardName({
  card,
  children,
  className,
}: {
  card: PreviewCard;
  children: ReactNode;
  className?: string;
}) {
  const { handlers } = useActiveCardTrigger(card);
  return (
    <span className={className} {...handlers}>
      {children}
    </span>
  );
}
