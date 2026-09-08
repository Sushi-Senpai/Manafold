"use client";

// Lightweight mana-cost and colour-identity rendering for the builder. The repo
// has no pip/symbol component yet, so this keeps it deliberately small: each
// `{…}` group in a Scryfall mana string becomes a chip, WUBRG(C) letters filled
// with their `--color-mana-*` token, everything else (generic, X, hybrid,
// Phyrexian) a neutral chip that still shows the raw symbol text.
//
// @spec DECK-080

const MANA_TOKEN: Record<string, string> = {
  W: "var(--color-mana-w)",
  U: "var(--color-mana-u)",
  B: "var(--color-mana-b)",
  R: "var(--color-mana-r)",
  G: "var(--color-mana-g)",
  C: "var(--color-mana-c)",
};

// Letters whose token colour is light enough to need dark ink.
const LIGHT_TOKENS = new Set(["W", "C"]);

function symbols(manaCost: string): string[] {
  return Array.from(manaCost.matchAll(/\{([^}]+)\}/g), (m) => m[1]);
}

export function ManaCost({ cost, className }: { cost: string | null; className?: string }) {
  if (!cost) return null;
  const parts = symbols(cost);
  if (parts.length === 0) return null;
  return (
    <span className={`inline-flex flex-wrap items-center gap-0.5 align-middle ${className ?? ""}`}>
      {parts.map((sym, i) => {
        const token = MANA_TOKEN[sym];
        return (
          <span
            key={`${sym}-${i}`}
            className={`inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] font-semibold leading-none ${
              token
                ? LIGHT_TOKENS.has(sym)
                  ? "text-black/80 ring-1 ring-black/10"
                  : "text-white"
                : "bg-surface-2 text-muted"
            }`}
            style={token ? { backgroundColor: token } : undefined}
            aria-hidden
          >
            {sym}
          </span>
        );
      })}
      <span className="sr-only">{`mana cost ${parts.join(" ")}`}</span>
    </span>
  );
}

export function ColorIdentity({ colors, className }: { colors: string[]; className?: string }) {
  return (
    <span className={`inline-flex items-center gap-0.5 align-middle ${className ?? ""}`}>
      {colors.length === 0 ? (
        <span className="text-[10px] font-medium text-muted">C</span>
      ) : (
        colors.map((c) => (
          <span
            key={c}
            title={c}
            className={`inline-block h-2.5 w-2.5 rounded-full ${
              LIGHT_TOKENS.has(c) ? "ring-1 ring-black/15" : ""
            }`}
            style={{ backgroundColor: MANA_TOKEN[c] ?? "var(--color-mana-c)" }}
          />
        ))
      )}
    </span>
  );
}
