"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";

import { api, type SessionState } from "@/lib/api";
import { signOut } from "@/lib/auth";

// Header identity control for the builder. The app is fully usable without an
// account (anonymous drafts, ACCT-020), so this shows "Sign in" until there is
// a session, then the account email and a sign-out button. It never gates the
// page — GET /api/auth/session never 401s (ACCT-014).
//
// @spec ACCT-014
export function HeaderAuth() {
  const router = useRouter();
  const [state, setState] = useState<SessionState | null>(null);

  useEffect(() => {
    api
      .getSession()
      .then(setState)
      .catch(() => setState({ authenticated: false }));
  }, []);

  if (state === null) {
    return <span className="text-xs text-muted">…</span>;
  }

  if (!state.authenticated) {
    return (
      <div className="flex items-center gap-3 text-sm">
        <Link href="/login" className="text-muted hover:text-foreground">
          Sign in
        </Link>
        <Link
          href="/register"
          className="rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-ink transition hover:opacity-90"
        >
          Create account
        </Link>
      </div>
    );
  }

  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="text-muted">{state.email}</span>
      <button
        type="button"
        onClick={async () => {
          await signOut();
          setState({ authenticated: false });
          router.push("/");
          router.refresh();
        }}
        className="text-muted hover:text-foreground"
      >
        Sign out
      </button>
    </div>
  );
}
