"use client";

import { useEffect, useState } from "react";
import {
  api,
  ApiError,
  type DeckDetail,
  type DeckTemplate,
  type WizardBasics,
  type WizardPlan,
  type WizardProposal,
  type WizardProposeResponse,
  type WizardStep,
} from "@/lib/api";
import {
  BRACKET_OPTIONS,
  STEP_LABELS,
  afterAccept,
  applyBasicsPlan,
  defaultAnswers,
  emptyReasonText,
  gameChangerLine,
  picksNote,
  proposeBody,
  rejectCard,
  removeProposal,
  visibleSteps,
  wizardErrorText,
  type WizardAnswers,
  type WizardStepId,
} from "@/lib/wizard";
import { ProposalList } from "./ProposalList";

// The deck wizard: an interactive scaffolder, never a generator. It walks the
// player through bracket and restrictions, their land comfort (including "basics
// only"), utility lands, and rounds of synergy picks, finishing with a basics
// plan. Each round's cards come from a deterministic pool the backend has
// already held to the bracket and the player's rules; the model only picks and
// explains. Every card enters the deck on the player's Accept, through the
// builder's own add-card path. It replaces the AI suggestions panel while open
// (WIZ-060).
//
// @spec WIZ-060, WIZ-061, WIZ-062, WIZ-063, WIZ-064, WIZ-065, WIZ-066, WIZ-067
export function WizardPanel({
  deckId,
  detail,
  onChange,
  onClose,
}: {
  deckId: string;
  detail: DeckDetail;
  onChange: () => void;
  onClose: () => void;
}) {
  const [plan, setPlan] = useState<WizardPlan | null>(null);
  const [templates, setTemplates] = useState<DeckTemplate[] | null>(null);
  const [step, setStep] = useState<WizardStepId>("setup");
  const [answers, setAnswers] = useState<WizardAnswers>(defaultAnswers);
  const [rejected, setRejected] = useState<string[]>([]);

  const [round, setRound] = useState<WizardProposeResponse | null>(null);
  const [proposals, setProposals] = useState<WizardProposal[]>([]);
  const [gcLeft, setGcLeft] = useState<number | null>(null);
  const [basics, setBasics] = useState<WizardBasics | null>(null);

  const [busy, setBusy] = useState(false);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const update = (patch: Partial<WizardAnswers>) => setAnswers((a) => ({ ...a, ...patch }));

  // The plan follows the deck: it is read again whenever the commander, bracket,
  // archetype or main board changes, so the Game Changer budget and land counts
  // stay current as the player accepts cards.
  const signature = JSON.stringify({
    c: detail.commander?.id ?? null,
    b: detail.bracket,
    a: detail.archetype,
    m: detail.boards.main.map((e) => `${e.card_id}:${e.quantity}`).join(","),
  });
  useEffect(() => {
    let cancelled = false;
    api
      .getWizardPlan(deckId)
      .then((p) => {
        if (!cancelled) setPlan(p);
      })
      .catch((e) => {
        if (!cancelled) setMessage(wizardErrorText(e));
      });
    return () => {
      cancelled = true;
    };
  }, [deckId, signature]);

  useEffect(() => {
    let cancelled = false;
    api
      .getDeckTemplates()
      .then((t) => {
        if (!cancelled) setTemplates(t.templates);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  // The basics plan is read on entering that step and after the deck or the land
  // target changes.
  useEffect(() => {
    if (step !== "basics") return;
    let cancelled = false;
    api
      .wizardBasics(deckId, answers.landTarget ?? undefined)
      .then((b) => {
        if (!cancelled) setBasics(b);
      })
      .catch((e) => {
        if (!cancelled) setMessage(wizardErrorText(e));
      });
    return () => {
      cancelled = true;
    };
  }, [step, deckId, answers.landTarget, signature]);

  const steps = visibleSteps(plan);
  const index = Math.max(0, steps.indexOf(step));

  function go(to: WizardStepId) {
    setStep(to);
    setRound(null);
    setProposals([]);
    setMessage(null);
  }
  const next = () => go(steps[Math.min(index + 1, steps.length - 1)]);
  const back = () => go(steps[Math.max(index - 1, 0)]);

  async function save(patch: { bracket?: number; archetype?: string }) {
    setBusy(true);
    setMessage(null);
    try {
      await api.updateDeck(deckId, patch);
      onChange();
    } catch (e) {
      setMessage(e instanceof ApiError ? e.message : "Could not save");
    } finally {
      setBusy(false);
    }
  }

  async function propose(which: WizardStep) {
    setBusy(true);
    setMessage(null);
    try {
      const resp = await api.wizardPropose(deckId, proposeBody(which, answers, rejected));
      setRound(resp);
      setProposals(resp.proposals);
      setGcLeft(resp.game_changers.left);
    } catch (e) {
      setMessage(wizardErrorText(e));
    } finally {
      setBusy(false);
    }
  }

  async function accept(p: WizardProposal) {
    setPendingId(p.card.id);
    setMessage(null);
    try {
      await api.addCard(deckId, p.card.id, "main");
      setProposals((list) => removeProposal(list, p.card.id));
      setGcLeft((left) => afterAccept(left, p));
      onChange();
    } catch (e) {
      setMessage(e instanceof ApiError ? e.message : "Could not add the card");
    } finally {
      setPendingId(null);
    }
  }

  function reject(p: WizardProposal) {
    setRejected((r) => rejectCard(r, p.card.id));
    setProposals((list) => removeProposal(list, p.card.id));
  }

  async function addBasics() {
    if (!basics) return;
    setBusy(true);
    setMessage(null);
    try {
      for (const action of applyBasicsPlan(basics)) {
        if (action.add) await api.addCard(deckId, action.cardId, "main");
        if (action.quantity !== 1) await api.setCardQuantity(deckId, action.cardId, "main", action.quantity);
      }
      setDone(true);
      onChange();
    } catch (e) {
      setMessage(e instanceof ApiError ? e.message : "Could not add the basic lands");
    } finally {
      setBusy(false);
    }
  }

  const aiOff = plan !== null && !plan.ai_enabled;

  return (
    <div className="rounded-lg border border-primary/30 bg-primary/5 p-4">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-primary">Deck wizard</h2>
          <p className="text-xs text-muted">
            Step {index + 1} of {steps.length} · {STEP_LABELS[step]}
          </p>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close the wizard"
          className="rounded-md border border-border px-2 py-1 text-xs"
        >
          Close
        </button>
      </div>

      {message && <p className="mt-3 text-sm text-danger">{message}</p>}

      {step === "setup" && (
        <div className="mt-3 flex flex-col gap-3 text-sm">
          <p>
            {detail.commander ? (
              <>
                Building for <span className="font-medium">{detail.commander.name}</span>.
              </>
            ) : (
              <span className="text-muted">Pick a commander in the Commander box above to begin.</span>
            )}
          </p>

          <div className="flex flex-wrap gap-2">
            <label className="flex min-w-0 flex-1 flex-col gap-1">
              <span className="text-xs text-muted">Bracket</span>
              <select
                value={detail.bracket !== null && detail.bracket <= 4 ? String(detail.bracket) : ""}
                disabled={busy}
                onChange={(e) => save({ bracket: Number(e.target.value) })}
                className="rounded-md border border-border bg-surface-2 px-2 py-1"
              >
                <option value="" disabled>
                  Choose a bracket…
                </option>
                {BRACKET_OPTIONS.map((b) => (
                  <option key={b.value} value={b.value}>
                    {b.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex min-w-0 flex-1 flex-col gap-1">
              <span className="text-xs text-muted">Archetype (sets the land target)</span>
              <select
                value={detail.archetype ?? ""}
                disabled={busy || !templates}
                onChange={(e) => save({ archetype: e.target.value })}
                className="rounded-md border border-border bg-surface-2 px-2 py-1"
              >
                <option value="">None (37 lands)</option>
                {templates?.map((t) => (
                  <option key={t.key} value={t.key}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {detail.bracket === 5 && (
            <p className="text-xs text-warning">The wizard supports Brackets 1 through 4. Choose one of those to continue.</p>
          )}
          {plan?.policy && (
            <p className="text-xs text-muted">
              {plan.policy.guidance}{" "}
              {plan.policy.game_changer_limit === null
                ? "There is no Game Changer limit."
                : `Up to ${plan.policy.game_changer_limit} Game Changers.`}
            </p>
          )}

          <fieldset className="flex flex-col gap-2 rounded-md border border-border p-3">
            <legend className="px-1 text-xs text-muted">Restrictions</legend>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={answers.noTutors}
                onChange={(e) => update({ noTutors: e.target.checked })}
              />
              No tutors
            </label>
            <label className="flex items-center gap-2">
              <span className="whitespace-nowrap">Max price per card ($)</span>
              <input
                inputMode="decimal"
                value={answers.maxPriceUsd}
                onChange={(e) => update({ maxPriceUsd: e.target.value })}
                placeholder="no cap"
                className="w-24 rounded-md border border-border bg-surface-2 px-2 py-1"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs text-muted">Strategy notes (optional, steers the picks)</span>
              <textarea
                value={answers.notes}
                maxLength={500}
                rows={2}
                onChange={(e) => update({ notes: e.target.value })}
                placeholder="e.g. goblin tokens, sacrifice payoffs"
                className="rounded-md border border-border bg-surface-2 px-2 py-1"
              />
            </label>
          </fieldset>

          {plan && !plan.ready && plan.reason === "no_bracket" && (
            <p className="text-xs text-muted">Choose a bracket to continue.</p>
          )}
          <button
            type="button"
            onClick={next}
            disabled={!plan?.ready}
            className="self-start rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-ink disabled:opacity-40"
          >
            Continue
          </button>
        </div>
      )}

      {step === "fixing" && (
        <div className="mt-3 flex flex-col gap-3 text-sm">
          <p className="font-medium">Which lands are you comfortable playing?</p>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="fixing"
              checked={answers.fixing === "basics"}
              onChange={() => update({ fixing: "basics" })}
            />
            Basics only (no dual lands)
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="fixing"
              checked={answers.fixing === "include"}
              onChange={() => update({ fixing: "include" })}
            />
            Include dual and other fixing lands
          </label>
          {answers.fixing === "include" && (
            <label className="ml-6 flex items-center gap-2">
              <input
                type="checkbox"
                checked={answers.includeFetch}
                onChange={(e) => update({ includeFetch: e.target.checked })}
              />
              Include fetch lands
            </label>
          )}
          <LandRound
            enabled={answers.fixing === "include"}
            aiOff={aiOff}
            busy={busy}
            round={round}
            proposals={proposals}
            pendingId={pendingId}
            gcLeft={gcLeft}
            label="Suggest fixing lands"
            onPropose={() => propose("fixing")}
            onAccept={accept}
            onReject={reject}
          />
          <StepNav onBack={back} onNext={next} nextLabel="Continue" />
        </div>
      )}

      {step === "utility" && (
        <div className="mt-3 flex flex-col gap-3 text-sm">
          <p className="font-medium">Do you want any utility lands?</p>
          <label className="flex items-center gap-2">
            <input type="radio" name="utility" checked={!answers.utility} onChange={() => update({ utility: false })} />
            No, just fixing and basics
          </label>
          <label className="flex items-center gap-2">
            <input type="radio" name="utility" checked={answers.utility} onChange={() => update({ utility: true })} />
            Yes, suggest some (man-lands, colourless utility)
          </label>
          <LandRound
            enabled={answers.utility}
            aiOff={aiOff}
            busy={busy}
            round={round}
            proposals={proposals}
            pendingId={pendingId}
            gcLeft={gcLeft}
            label="Suggest utility lands"
            onPropose={() => propose("utility")}
            onAccept={accept}
            onReject={reject}
          />
          <StepNav onBack={back} onNext={next} nextLabel="Continue" />
        </div>
      )}

      {step === "picks" && (
        <div className="mt-3 flex flex-col gap-3 text-sm">
          <p className="font-medium">Cards that fit {detail.commander?.name ?? "your commander"}</p>
          {(round?.game_changers ?? plan?.policy) && (
            <p className="text-xs text-muted">
              {gameChangerLine(
                round?.game_changers ?? {
                  limit: plan!.policy!.game_changer_limit,
                  in_deck: plan!.policy!.game_changers_in_deck,
                  left: plan!.policy!.game_changers_left,
                },
              )}
            </p>
          )}
          {aiOff && <p className="text-xs text-muted">AI picks are switched off on this server.</p>}
          <button
            type="button"
            onClick={() => propose("picks")}
            disabled={busy || aiOff}
            className="self-start rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-ink disabled:opacity-40"
          >
            {busy ? "Thinking…" : round ? "More picks" : "Suggest picks"}
          </button>
          <RoundResult
            round={round}
            proposals={proposals}
            pendingId={pendingId}
            gcLeft={gcLeft}
            onAccept={accept}
            onReject={reject}
          />
          <StepNav onBack={back} onNext={next} nextLabel="Done — fill the lands" />
        </div>
      )}

      {step === "basics" && (
        <div className="mt-3 flex flex-col gap-3 text-sm">
          <p className="font-medium">Basic lands</p>
          {!basics && <p className="text-muted">Planning…</p>}
          {basics && (
            <>
              <label className="flex items-center gap-2">
                <span>Land target</span>
                <input
                  type="number"
                  min={20}
                  max={60}
                  defaultValue={basics.land_target}
                  key={basics.land_target}
                  onBlur={(e) => {
                    const n = Number(e.target.value);
                    if (Number.isInteger(n) && n >= 20 && n <= 60) update({ landTarget: n });
                  }}
                  className="w-20 rounded-md border border-border bg-surface-2 px-2 py-1"
                />
                <span className="text-xs text-muted">
                  {basics.lands_in_deck} lands now · {basics.basics_needed} basics to add
                </span>
              </label>
              {basics.rows.length > 0 ? (
                <ul className="flex flex-col gap-1 text-xs">
                  {basics.rows.map((r) => (
                    <li key={r.card.id} className="flex justify-between">
                      <span>{r.card.name}</span>
                      <span className="text-muted">
                        +{r.quantity}
                        {r.in_deck > 0 && ` (${r.in_deck} already in the deck)`}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-xs text-muted">The deck already has enough lands.</p>
              )}
              {basics.missing.length > 0 && (
                <p className="text-xs text-warning">Not in the card data: {basics.missing.join(", ")}.</p>
              )}
              <button
                type="button"
                onClick={addBasics}
                disabled={busy || basics.rows.length === 0 || done}
                className="self-start rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-ink disabled:opacity-40"
              >
                {busy ? "Adding…" : done ? "Basics added" : "Add these basic lands"}
              </button>
            </>
          )}
          {done && (
            <p className="text-xs text-muted">
              That is the end of the guided steps. Keep building in the builder: the left-hand panels show how the deck
              lines up with its archetype and bracket.
            </p>
          )}
          <StepNav onBack={back} onNext={onClose} nextLabel="Close the wizard" />
        </div>
      )}
    </div>
  );
}

// LandRound is the propose button and results of a land step. When the player
// declined the step it makes no request at all (WIZ-062).
function LandRound({
  enabled,
  aiOff,
  busy,
  round,
  proposals,
  pendingId,
  gcLeft,
  label,
  onPropose,
  onAccept,
  onReject,
}: {
  enabled: boolean;
  aiOff: boolean;
  busy: boolean;
  round: WizardProposeResponse | null;
  proposals: WizardProposal[];
  pendingId: string | null;
  gcLeft: number | null;
  label: string;
  onPropose: () => void;
  onAccept: (p: WizardProposal) => void;
  onReject: (p: WizardProposal) => void;
}) {
  if (!enabled) return null;
  return (
    <>
      {aiOff && <p className="text-xs text-muted">AI picks are switched off on this server.</p>}
      <button
        type="button"
        onClick={onPropose}
        disabled={busy || aiOff}
        className="self-start rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-ink disabled:opacity-40"
      >
        {busy ? "Thinking…" : round ? "Ask again" : label}
      </button>
      <RoundResult
        round={round}
        proposals={proposals}
        pendingId={pendingId}
        gcLeft={gcLeft}
        onAccept={onAccept}
        onReject={onReject}
      />
    </>
  );
}

// RoundResult shows the model's summary, the proposals still awaiting a
// decision, the empty-answer reason, and the data-source note (WIZ-065).
function RoundResult({
  round,
  proposals,
  pendingId,
  gcLeft,
  onAccept,
  onReject,
}: {
  round: WizardProposeResponse | null;
  proposals: WizardProposal[];
  pendingId: string | null;
  gcLeft: number | null;
  onAccept: (p: WizardProposal) => void;
  onReject: (p: WizardProposal) => void;
}) {
  if (!round) return null;
  const note = picksNote(round);
  return (
    <div>
      {round.summary && <p className="text-xs italic text-foreground/80">{round.summary}</p>}
      {round.empty_reason && <p className="mt-2 text-xs text-muted">{emptyReasonText(round.empty_reason)}</p>}
      {!round.empty_reason && proposals.length === 0 && (
        <p className="mt-2 text-xs text-muted">
          {round.proposals.length === 0
            ? "No suggestions survived the checks. Try again."
            : "All of this round's cards are decided."}
        </p>
      )}
      <ProposalList
        proposals={proposals}
        pendingId={pendingId}
        gcLeft={gcLeft}
        onAccept={onAccept}
        onReject={onReject}
      />
      {round.dropped > 0 && <p className="mt-2 text-xs text-muted">{round.dropped} suggested cards were filtered out.</p>}
      {!round.combos_checked && round.step === "picks" && (
        <p className="mt-1 text-xs text-muted">Combo data is not loaded, so combos were not checked.</p>
      )}
      {note?.kind === "synergy" && (
        <p className="mt-2 text-xs text-muted">
          <a href={note.url} target="_blank" rel="noreferrer" className="underline">
            {note.text}
          </a>{" "}
          · {note.decks} decks sampled
        </p>
      )}
      {note?.kind === "popular" && <p className="mt-2 text-xs text-muted">{note.text}</p>}
    </div>
  );
}

function StepNav({ onBack, onNext, nextLabel }: { onBack: () => void; onNext: () => void; nextLabel: string }) {
  return (
    <div className="mt-1 flex justify-between gap-2 border-t border-border pt-3">
      <button type="button" onClick={onBack} className="rounded-md border border-border px-3 py-1 text-xs">
        Back
      </button>
      <button type="button" onClick={onNext} className="rounded-md border border-border px-3 py-1 text-xs">
        {nextLabel}
      </button>
    </div>
  );
}
