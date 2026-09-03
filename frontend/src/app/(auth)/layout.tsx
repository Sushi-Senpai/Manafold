import Link from "next/link";

import { Wordmark } from "@/components/Wordmark";

// The sign-in / register surface uses the same light "workspace" theme as the
// builder so the palette is consistent once a visitor crosses from the landing
// page.
export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="workspace flex min-h-full flex-1 flex-col bg-background font-sans text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-6xl items-center px-6 py-4">
          <Link href="/" className="text-lg">
            <Wordmark />
          </Link>
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-6xl flex-1 items-center justify-center px-6 py-16">
        {children}
      </main>
    </div>
  );
}
