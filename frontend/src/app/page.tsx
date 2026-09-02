import Link from "next/link";

// M1 landing page. A plain text wordmark is fine for M1 (the real branded
// marketing surface and the email + password sign-in flow are M3). The one
// action is to open the deck list.
export default function Home() {
  return (
    <main className="relative flex flex-1 flex-col items-center justify-center gap-6 px-6 py-24 text-center">
      <h1 className="font-sans text-5xl font-semibold tracking-wide">Manafold</h1>
      <p className="max-w-xl text-balance text-foreground/70">
        A Commander-first Magic: The Gathering deckbuilder that enforces the
        format&apos;s rules while you build — colour identity, singleton, the
        100-card count, and the banlist, validated live.
      </p>
      <Link
        href="/decks"
        className="rounded-lg bg-primary px-6 py-3 text-sm font-semibold text-white transition hover:opacity-90"
      >
        Open your decks
      </Link>
      <footer className="mt-16 max-w-2xl text-xs leading-relaxed text-foreground/40">
        Card data and images © Wizards of the Coast, provided by{" "}
        <a href="https://scryfall.com" className="underline">
          Scryfall
        </a>
        . Manafold is unofficial Fan Content permitted under the Fan Content
        Policy. Not approved/endorsed by Wizards of the Coast. Portions of the
        materials used are property of Wizards of the Coast. ©Wizards of the
        Coast LLC.
      </footer>
    </main>
  );
}
