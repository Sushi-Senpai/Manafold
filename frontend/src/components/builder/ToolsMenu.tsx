"use client";

// The deck header's "Tools" control: a single button that opens the
// import/export dialog. Kept as its own component so the header stays a thin
// layout concern and the dialog's open state lives next to it.
//
// @spec DECK-095

import { useEffect, useRef, useState } from "react";

import { ImportExportDialog } from "./ImportExportDialog";

export function ToolsMenu({ deckId, onImported }: { deckId: string; onImported: () => void }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menuOpen) return;
    function onDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setMenuOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setMenuOpen(false);
    }
    document.addEventListener("mousedown", onDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("mousedown", onDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [menuOpen]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setMenuOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        className="rounded-md border border-border px-3 py-1.5 text-xs font-medium text-muted transition hover:border-primary hover:text-primary"
      >
        Tools ▾
      </button>
      {menuOpen && (
        <div
          role="menu"
          className="absolute right-0 z-40 mt-1 w-48 overflow-hidden rounded-lg border border-border bg-surface py-1 text-sm shadow-xl"
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setMenuOpen(false);
              setDialogOpen(true);
            }}
            className="block w-full px-3 py-1.5 text-left hover:bg-primary/10"
          >
            Import / export…
          </button>
        </div>
      )}
      {dialogOpen && (
        <ImportExportDialog
          deckId={deckId}
          onImported={onImported}
          onClose={() => setDialogOpen(false)}
        />
      )}
    </div>
  );
}
