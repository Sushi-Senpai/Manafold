import Link from "next/link";

import { HeaderAuth } from "@/components/HeaderAuth";
import { Wordmark } from "@/components/Wordmark";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="workspace min-h-full flex-1 bg-background font-sans text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-6xl items-center gap-6 px-6 py-4">
          <Link href="/" className="text-lg">
            <Wordmark />
          </Link>
          <nav className="flex flex-1 gap-4 text-sm">
            <Link href="/decks" className="text-muted hover:text-foreground">
              Decks
            </Link>
          </nav>
          <HeaderAuth />
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">{children}</main>
    </div>
  );
}
