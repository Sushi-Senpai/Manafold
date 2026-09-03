"use client";

import { useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";

import { ApiError } from "@/lib/api";
import { signIn, signUp } from "@/lib/auth";

type Mode = "login" | "register";

const COPY: Record<Mode, { title: string; cta: string; alt: string; altHref: string; altLabel: string }> = {
  login: {
    title: "Sign in",
    cta: "Sign in",
    alt: "New to Manafold?",
    altHref: "/register",
    altLabel: "Create an account",
  },
  register: {
    title: "Create your account",
    cta: "Create account",
    alt: "Already have an account?",
    altHref: "/login",
    altLabel: "Sign in",
  },
};

// @spec ACCT-010, ACCT-011
export function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const params = useSearchParams();
  const next = params.get("next") || "/decks";
  const copy = COPY[mode];

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const run = mode === "login" ? signIn : signUp;
      await run(email.trim(), password);
      router.push(next);
      router.refresh();
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError) {
        setError(
          err.status === 422
            ? "Enter a valid email and a password of at least 10 characters."
            : err.status === 429
              ? "Too many attempts. Wait a moment and try again."
              : err.message,
        );
      } else {
        setError("Something went wrong. Try again.");
      }
    }
  }

  return (
    <form onSubmit={submit} className="flex w-full max-w-sm flex-col gap-4">
      <h1 className="text-2xl font-semibold">{copy.title}</h1>

      <label className="flex flex-col gap-1 text-sm">
        <span className="text-foreground/70">Email</span>
        <input
          type="email"
          autoComplete="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className="rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-primary"
        />
      </label>

      <label className="flex flex-col gap-1 text-sm">
        <span className="text-foreground/70">Password</span>
        <input
          type="password"
          autoComplete={mode === "login" ? "current-password" : "new-password"}
          required
          minLength={mode === "register" ? 10 : undefined}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="rounded-lg border border-border bg-surface px-3 py-2 text-sm outline-none focus:border-primary"
        />
        {mode === "register" && (
          <span className="text-xs text-foreground/50">At least 10 characters.</span>
        )}
      </label>

      {error && <p className="text-sm text-danger">{error}</p>}

      <button
        type="submit"
        disabled={busy}
        className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-ink transition hover:opacity-90 disabled:opacity-40"
      >
        {busy ? "…" : copy.cta}
      </button>

      <p className="text-sm text-foreground/60">
        {copy.alt}{" "}
        <Link href={copy.altHref} className="text-primary underline">
          {copy.altLabel}
        </Link>
      </p>
    </form>
  );
}
