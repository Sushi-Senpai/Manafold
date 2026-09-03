// Client-side helpers for the email + password flow. Each wraps the raw
// `api.*` call and, on a successful register or login, hands this browser's
// anonymous-draft token to `POST /api/auth/claim-drafts` so any decks built
// before signing in follow the new account (ACCT-021).
//
// @spec ACCT-021

import { api, getAnonToken, type SessionState } from "@/lib/api";

async function claimAnyDrafts(): Promise<void> {
  const token = getAnonToken();
  if (!token) return;
  try {
    await api.claimDrafts(token);
  } catch {
    // A failed claim must not block sign-in; the drafts stay under the token
    // and a later claim can retry.
  }
}

export async function signUp(email: string, password: string): Promise<SessionState> {
  const state = await api.register(email, password);
  await claimAnyDrafts();
  return state;
}

export async function signIn(email: string, password: string): Promise<SessionState> {
  const state = await api.login(email, password);
  await claimAnyDrafts();
  return state;
}

export async function signOut(): Promise<void> {
  await api.logout();
}
