// The "Manafold" text wordmark: display weight for the "Mana" stem, a lighter
// faded weight for the "fold" tail (brand direction "Slate & Signet", styling
// in globals.css). This is the approved plain-text mark; the captain's vector
// mark is still pending (PLATFORM-024, deferred).
export function Wordmark({ className = "" }: { className?: string }) {
  return (
    <span className={`wordmark ${className}`}>
      Mana<span className="wordmark-tail">fold</span>
    </span>
  );
}
