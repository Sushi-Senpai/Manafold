import Link from "next/link";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="workspace min-h-full flex-1 bg-background font-sans text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-6xl items-center gap-6 px-6 py-4">
          <Link href="/" className="text-lg font-semibold tracking-wide">
            Manafold
          </Link>
          <nav className="flex gap-4 text-sm">
            <Link href="/decks" className="text-foreground/70 hover:text-foreground">
              Decks
            </Link>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">{children}</main>
    </div>
  );
}
