import Link from "next/link";

import { Wordmark } from "@/components/Wordmark";

// Landing page. The plain-text wordmark is the approved mark (the captain's
// vector artwork is still pending, PLATFORM-024). A visitor can jump straight
// into building (anonymous drafts, ACCT-020) or sign in / create an account.
export default function Home() {
  return (
    <main className="relative flex flex-1 flex-col items-center justify-center gap-6 px-6 py-24 text-center">
      <h1 className="text-6xl">
        <Wordmark />
      </h1>
      <p className="max-w-xl text-balance text-foreground/70">
        A Commander-first Magic: The Gathering deckbuilder that enforces the
        format&apos;s rules while you build — colour identity, singleton, the
        100-card count, and the banlist, validated live.
      </p>
      <div className="flex flex-wrap items-center justify-center gap-3">
        <Link
          href="/decks"
          className="rounded-lg bg-primary px-6 py-3 text-sm font-semibold text-primary-ink transition hover:opacity-90"
        >
          Start building
        </Link>
        <Link
          href="/login"
          className="rounded-lg border border-foreground/20 px-6 py-3 text-sm font-semibold transition hover:border-foreground/40"
        >
          Sign in
        </Link>
      </div>
      <p className="text-xs text-foreground/40">
        No account needed to start — your deck is saved to this browser and
        follows you in when you sign up.
      </p>
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
