"use client";

// The builder's shared "which card is the image panel showing" state. Any
// card-name surface calls `useActiveCard().set(key, card)` on hover / focus and
// `.clear(key)` on leave / blur; the sticky CardImagePanel reads `.active`.
// `page.tsx` mounts the provider once around the whole builder subtree — which
// already sits inside the `(app)` layout's `.workspace` wrapper, so there is no
// portal and no palette re-scoping (unlike the old floating preview).
//
// @spec DECK-070, DECK-072, DECK-074, DECK-076

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import type { PreviewCard } from "@/lib/cardPreview";

export type { PreviewCard };

// How long a card-name surface's "clear" waits before it actually blanks the
// panel. Long enough that moving the pointer across the gap between two adjacent
// rows hands the panel straight from one card to the next without a flash back
// to the commander (DECK-074); short enough that leaving the list feels
// immediate.
const CLEAR_DELAY_MS = 90;

type ActiveCardApi = {
  active: PreviewCard | null;
  // `key` scopes the two calls to one surface: `clear` only blanks the panel
  // when the key still matches what is showing, so a late blur from the row the
  // pointer already left cannot wipe the card the next row just set.
  set: (key: string, card: PreviewCard) => void;
  clear: (key: string) => void;
};

const ActiveCardContext = createContext<ActiveCardApi | null>(null);

export function ActiveCardProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{ key: string; card: PreviewCard } | null>(null);
  const clearTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancelClear = useCallback(() => {
    if (clearTimer.current) {
      clearTimeout(clearTimer.current);
      clearTimer.current = null;
    }
  }, []);

  const set = useCallback<ActiveCardApi["set"]>(
    (key, card) => {
      cancelClear();
      setState({ key, card });
    },
    [cancelClear],
  );

  const clear = useCallback<ActiveCardApi["clear"]>(
    (key) => {
      cancelClear();
      clearTimer.current = setTimeout(() => {
        clearTimer.current = null;
        setState((prev) => (prev?.key === key ? null : prev));
      }, CLEAR_DELAY_MS);
    },
    [cancelClear],
  );

  const api = useMemo<ActiveCardApi>(
    () => ({ active: state?.card ?? null, set, clear }),
    [state, set, clear],
  );

  return <ActiveCardContext.Provider value={api}>{children}</ActiveCardContext.Provider>;
}

// useActiveCard returns the shared API. Outside a provider it is an inert
// no-op, so a card-name surface rendered on its own (a test, a future page)
// degrades to plain text rather than throwing.
export function useActiveCard(): ActiveCardApi {
  return (
    useContext(ActiveCardContext) ?? {
      active: null,
      set: () => {},
      clear: () => {},
    }
  );
}
