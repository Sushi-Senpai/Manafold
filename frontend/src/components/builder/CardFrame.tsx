"use client";

// CardFrame stands in for a card the mirror has no printing image for, so the
// image panel still shows something identifying instead of a broken rectangle.
// Shared by CardImagePanel; kept its own file so it can be reused and tested in
// isolation.
//
// @spec DECK-071

import type { PreviewCard } from "@/lib/cardPreview";

export function CardFrame({ card }: { card: PreviewCard }) {
  return (
    <div className="flex h-full w-full flex-col gap-2 rounded-xl border border-border bg-surface-2 p-4">
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
