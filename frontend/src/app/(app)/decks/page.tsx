"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { api, ApiError, type Deck } from "@/lib/api";

// @spec DECK-001, WIZ-060
export default function DecksPage() {
  const router = useRouter();
  const [decks, setDecks] = useState<Deck[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    api
      .listDecks()
      .then((r) => setDecks(r.decks))
      .catch((e) => setError(e instanceof ApiError ? e.message : "Failed to load decks"));
  }, []);

  // withWizard opens the new deck with the deck wizard's panel (WIZ-060).
  async function createDeck(e: React.FormEvent | null, withWizard = false) {
    e?.preventDefault();
    setCreating(true);
    setError(null);
    try {
      const deck = await api.createDeck(name.trim() || "Untitled deck");
      router.push(withWizard ? `/decks/${deck.id}?wizard=1` : `/decks/${deck.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to create deck");
      setCreating(false);
    }
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-8">
      <div>
        <h1 className="text-2xl font-semibold">Your decks</h1>
        <p className="mt-1 text-sm text-foreground/60">
          Commander decks with live colour-identity, singleton, count, and banlist validation.
        </p>
      </div>

      <form onSubmit={createDeck} className="flex gap-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="New deck name"
          className="flex-1 rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-primary"
        />
        <button
          type="submit"
          disabled={creating}
          className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-ink transition hover:opacity-90 disabled:opacity-40"
        >
          {creating ? "Creating…" : "New deck"}
        </button>
        <button
          type="button"
          disabled={creating}
          onClick={() => createDeck(null, true)}
          className="rounded-lg border border-primary/40 px-4 py-2 text-sm font-medium text-primary transition hover:bg-primary/5 disabled:opacity-40"
        >
          New deck with wizard
        </button>
      </form>

      {error && <p className="text-sm text-red-600">{error}</p>}

      {decks === null && !error && <p className="text-sm text-foreground/40">Loading…</p>}

      {decks && decks.length === 0 && (
        <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-foreground/50">
          No decks yet. Name one above to start building.
        </p>
      )}

      {decks && decks.length > 0 && (
        <ul className="flex flex-col gap-2">
          {decks.map((deck) => (
            <li key={deck.id}>
              <Link
                href={`/decks/${deck.id}`}
                className="flex items-center justify-between rounded-lg border border-border bg-surface px-4 py-3 transition hover:border-primary/50"
              >
                <span className="font-medium">{deck.name}</span>
                <span className="text-xs text-foreground/50">
                  {deck.color_identity.length > 0 ? deck.color_identity.join("") : "colourless"}
                  {deck.is_public && " · public"}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
