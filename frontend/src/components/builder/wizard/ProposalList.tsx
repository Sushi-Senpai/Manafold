"use client";

import type { WizardProposal } from "@/lib/api";
import { canAccept } from "@/lib/wizard";
import { HoverCardName } from "../HoverCardName";
import { ManaCost } from "../ManaSymbols";

// One round of the wizard's proposals. Each card is a real mirror card the
// backend has already proven legal and in-bracket; the player decides. Accept
// adds it to the deck through the ordinary add-card path; Reject remembers it so
// later rounds leave it out (WIZ-063). Nothing is added on its own.
//
// @spec WIZ-063, WIZ-064
export function ProposalList({
  proposals,
  pendingId,
  gcLeft,
  onAccept,
  onReject,
}: {
  proposals: WizardProposal[];
  pendingId: string | null;
  gcLeft: number | null;
  onAccept: (p: WizardProposal) => void;
  onReject: (p: WizardProposal) => void;
}) {
  return (
    <ul className="mt-3 flex flex-col gap-3">
      {proposals.map((p) => {
        const acceptable = canAccept(p, gcLeft);
        return (
          <li key={p.card.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <HoverCardName card={p.card} className="font-medium">
                    {p.card.name}
                  </HoverCardName>
                  <ManaCost cost={p.card.mana_cost} />
                  {p.game_changer && (
                    <span className="rounded bg-warning/15 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-warning">
                      Game Changer
                    </span>
                  )}
                </div>
                <div className="text-xs text-muted">
                  {p.card.type_line}
                  {p.price_usd !== null && ` · $${p.price_usd.toFixed(2)}`}
                  {p.synergy && ` · in ${Math.round(p.synergy.inclusion * 100)}% of decks`}
                  {p.synergy?.synergy != null && ` · ${formatPoints(p.synergy.synergy)} synergy`}
                </div>
                <p className="mt-1 text-xs text-foreground/80">{p.reason}</p>
              </div>
              <div className="flex shrink-0 flex-col gap-1">
                <button
                  type="button"
                  onClick={() => onAccept(p)}
                  disabled={!acceptable || pendingId === p.card.id}
                  title={acceptable ? undefined : "No Game Changers left in this bracket"}
                  className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-primary-ink disabled:opacity-40"
                >
                  Accept
                </button>
                <button
                  type="button"
                  onClick={() => onReject(p)}
                  disabled={pendingId === p.card.id}
                  className="rounded-md border border-border px-3 py-1 text-xs disabled:opacity-40"
                >
                  Reject
                </button>
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

function formatPoints(synergy: number): string {
  const pts = Math.round(synergy * 100);
  return `${pts >= 0 ? "+" : ""}${pts}`;
}
