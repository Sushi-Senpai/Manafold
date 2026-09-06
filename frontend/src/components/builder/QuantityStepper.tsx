"use client";

// A compact  [−] N [+]  quantity control for a decklist entry. It is policy-free:
// the parent decides what increment / decrement do and whether decrement is
// available (some rows can only be removed whole — see Decklist).
//
// @spec DECK-086

export function QuantityStepper({
  quantity,
  onIncrement,
  onDecrement,
  canDecrement,
  decrementTitle,
  incrementTitle,
}: {
  quantity: number;
  onIncrement: () => void;
  onDecrement: () => void;
  canDecrement: boolean;
  decrementTitle?: string;
  incrementTitle?: string;
}) {
  return (
    <span className="inline-flex shrink-0 items-center rounded-md border border-border">
      <button
        type="button"
        onClick={onDecrement}
        disabled={!canDecrement}
        title={decrementTitle}
        aria-label={quantity <= 1 ? "Remove card" : "Remove one copy"}
        className="px-1.5 py-0.5 text-xs text-muted transition hover:text-danger disabled:cursor-not-allowed disabled:opacity-30"
      >
        {quantity <= 1 ? "✕" : "−"}
      </button>
      <span className="min-w-5 border-x border-border px-1 text-center text-xs font-medium tabular-nums">
        {quantity}
      </span>
      <button
        type="button"
        onClick={onIncrement}
        title={incrementTitle}
        aria-label="Add one copy"
        className="px-1.5 py-0.5 text-xs text-muted transition hover:text-primary"
      >
        +
      </button>
    </span>
  );
}
