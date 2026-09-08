"use client";

// Import / export, moved off the always-on builder page into a dialog opened
// from the deck header's Tools menu (DECK-095). Parse-then-apply against
// POST /import (+ /import/{id}/apply) and text export via GET /export — the
// import-export segment's endpoints; the dialog is just their UI.
//
// @spec PORT-001, PORT-004, PORT-006, PORT-007, DECK-095

import { useEffect, useState } from "react";

import { api, ApiError, type ImportFormat, type ImportPreview } from "@/lib/api";

const IMPORT_FORMATS: { value: ImportFormat; label: string }[] = [
  { value: "plaintext", label: "Plain text" },
  { value: "mtga", label: "MTG Arena" },
  { value: "moxfield", label: "Moxfield" },
  { value: "archidekt", label: "Archidekt" },
];

export function ImportExportDialog({
  deckId,
  onImported,
  onClose,
}: {
  deckId: string;
  onImported: () => void;
  onClose: () => void;
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:p-10"
      role="dialog"
      aria-modal="true"
      aria-label="Import and export"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-lg rounded-lg border border-border bg-surface p-5 shadow-2xl">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">
            Import / export
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded px-1.5 text-muted transition hover:text-foreground"
          >
            ✕
          </button>
        </div>
        <ImportExportBody deckId={deckId} onImported={onImported} />
      </div>
    </div>
  );
}

function ImportExportBody({ deckId, onImported }: { deckId: string; onImported: () => void }) {
  const [format, setFormat] = useState<ImportFormat>("plaintext");
  const [raw, setRaw] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [exported, setExported] = useState("");

  async function doParse() {
    setBusy(true);
    setMsg(null);
    try {
      setPreview(await api.parseImport(deckId, format, raw));
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Parse failed");
    } finally {
      setBusy(false);
    }
  }

  async function doApply() {
    if (!preview) return;
    setBusy(true);
    setMsg(null);
    try {
      await api.applyImport(deckId, preview.import_id);
      setPreview(null);
      setRaw("");
      setMsg("Imported.");
      onImported();
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Apply failed");
    } finally {
      setBusy(false);
    }
  }

  async function doExport(fmt: "plaintext" | "mtga") {
    setMsg(null);
    try {
      setExported(await api.exportDeck(deckId, fmt));
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : "Export failed");
    }
  }

  return (
    <>
      <div className="mt-3 flex items-center gap-2">
        <select
          value={format}
          onChange={(e) => setFormat(e.target.value as ImportFormat)}
          className="rounded-lg border border-border bg-background px-2 py-1.5 text-sm"
        >
          {IMPORT_FORMATS.map((f) => (
            <option key={f.value} value={f.value}>
              {f.label}
            </option>
          ))}
        </select>
        <button
          onClick={doParse}
          disabled={busy || raw.trim() === ""}
          className="rounded-md border border-border px-2 py-1.5 text-xs font-medium transition hover:border-primary hover:text-primary disabled:opacity-40"
        >
          Preview import
        </button>
      </div>

      <textarea
        value={raw}
        onChange={(e) => setRaw(e.target.value)}
        rows={5}
        placeholder={"Commander:\n1 Atraxa, Praetors' Voice\n\nDeck\n1 Sol Ring\n…"}
        className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary"
      />

      {preview && (
        <div className="mt-3 rounded-md border border-border bg-surface-2 p-3 text-xs">
          <p className="font-medium">
            {preview.resolved.length} resolved · {preview.unresolved.length} unresolved
            {preview.rejected.length > 0 && ` · ${preview.rejected.length} unreadable`}
          </p>
          {preview.unresolved.length > 0 && (
            <ul className="mt-1 list-disc pl-4 text-warning">
              {preview.unresolved.map((u, i) => (
                <li key={i}>
                  {u.quantity}× {u.name}
                </li>
              ))}
            </ul>
          )}
          <button
            onClick={doApply}
            disabled={busy || preview.resolved.length === 0}
            className="mt-2 rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-ink transition hover:opacity-90 disabled:opacity-40"
          >
            Add {preview.resolved.length} cards to the deck
          </button>
        </div>
      )}

      <div className="mt-3 flex items-center gap-2">
        <span className="text-xs text-muted">Export:</span>
        <button
          onClick={() => doExport("plaintext")}
          className="rounded-md border border-border px-2 py-1 text-xs font-medium hover:border-primary hover:text-primary"
        >
          Plain text
        </button>
        <button
          onClick={() => doExport("mtga")}
          className="rounded-md border border-border px-2 py-1 text-xs font-medium hover:border-primary hover:text-primary"
        >
          MTG Arena
        </button>
      </div>
      {exported && (
        <textarea
          readOnly
          value={exported}
          rows={5}
          className="mt-2 w-full rounded-lg border border-border bg-surface-2 px-3 py-2 font-mono text-xs"
        />
      )}
      {msg && <p className="mt-2 text-xs text-muted">{msg}</p>}
    </>
  );
}
