"use client";

// The builder's card-image preview provider. Any card-name surface calls
// `useCardPreview().show(card, anchorEl)` to raise a floating preview near the
// pointer; the provider owns the single preview instance, the global
// scroll / Escape dismissal, and the coarse-pointer gate. `page.tsx` mounts the
// provider once and otherwise stays out of preview concerns.
//
// @spec DECK-070, DECK-074, DECK-075, DECK-076

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import type { AnchorRect } from "@/lib/cardPreview";
import { CardHoverPreview } from "./CardHoverPreview";

export type PreviewCard = {
  name: string;
  mana_cost: string | null;
  type_line: string;
  image_uris: { small?: string; normal?: string; large?: string } | null;
};

type ActivePreview = { card: PreviewCard; anchor: AnchorRect; key: string };

type CardPreviewApi = {
  // hoverable is false on coarse / no-hover pointers; callers use it to skip
  // arming their hover handlers entirely (DECK-075).
  hoverable: boolean;
  show: (key: string, card: PreviewCard, anchorEl: Element) => void;
  // hide clears the preview only when `key` matches what is currently shown, so
  // a late mouse-leave from a row the pointer already left cannot dismiss the
  // preview raised by the row it moved onto.
  hide: (key: string) => void;
};

const CardPreviewContext = createContext<CardPreviewApi | null>(null);

function readAnchor(el: Element): AnchorRect {
  const r = el.getBoundingClientRect();
  return { top: r.top, left: r.left, width: r.width, height: r.height };
}

export function CardPreviewProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<ActivePreview | null>(null);
  const [hoverable, setHoverable] = useState(false);

  // Only arm previews where the primary pointer can actually hover. Evaluated
  // after mount (never during SSR) and kept live if the pointer type changes
  // (e.g. a tablet gaining a mouse).
  useEffect(() => {
    const mq = window.matchMedia("(hover: hover) and (pointer: fine)");
    const sync = () => setHoverable(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);

  const show = useCallback<CardPreviewApi["show"]>((key, card, anchorEl) => {
    setActive({ key, card, anchor: readAnchor(anchorEl) });
  }, []);

  const hide = useCallback<CardPreviewApi["hide"]>((key) => {
    setActive((prev) => (prev?.key === key ? null : prev));
  }, []);

  const clear = useCallback(() => setActive(null), []);

  // Global dismissal: any scroll (including inside the search / decklist scroll
  // panes, hence capture) and the Escape key close the preview (DECK-074).
  useEffect(() => {
    if (!active) return;
    const onScroll = () => clear();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") clear();
    };
    window.addEventListener("scroll", onScroll, { capture: true, passive: true });
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("scroll", onScroll, { capture: true });
      window.removeEventListener("keydown", onKey);
    };
  }, [active, clear]);

  const api = useMemo<CardPreviewApi>(() => ({ hoverable, show, hide }), [hoverable, show, hide]);

  return (
    <CardPreviewContext.Provider value={api}>
      {children}
      {active && <CardHoverPreview card={active.card} anchor={active.anchor} />}
    </CardPreviewContext.Provider>
  );
}

// useCardPreview returns the preview API. Outside a provider it returns a
// no-op with `hoverable: false`, so a card-name surface rendered on its own
// (e.g. in a test or a future page) degrades to plain text rather than throwing.
export function useCardPreview(): CardPreviewApi {
  return (
    useContext(CardPreviewContext) ?? {
      hoverable: false,
      show: () => {},
      hide: () => {},
    }
  );
}
