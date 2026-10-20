// Pure helpers for the builder's synergy trigger. Kept out of the component so
// they can be unit-tested (see synergy.test.ts).
//
// @spec SYN-044

// ensureOnce asks the backend to gather synergy data for a deck's commander the
// first time that commander is seen, and never again for it in this page's
// lifetime. The answer is deliberately dropped — the builder shows nothing from
// it — and a failure is swallowed, because gathering is best-effort background
// work, not something the deck depends on.
//
// @spec SYN-044
export function ensureOnce(
  done: Set<string>,
  commanderId: string | null,
  ensure: (commanderId: string) => Promise<unknown>,
): void {
  if (!commanderId || done.has(commanderId)) return;
  done.add(commanderId);
  ensure(commanderId).catch(() => {});
}
