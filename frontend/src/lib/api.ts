// The single fetch wrapper every page calls. It reaches the Go backend only
// through the same-origin /api/* rewrite (next.config.ts, ACCT-016).
//
// @spec PLATFORM-021

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const ANON_TOKEN_KEY = "manafold_anon";
const AUTHED_KEY = "manafold_authed";

// setAuthed records whether this browser currently believes a session is live.
// request() suppresses the anonymous-draft token while this is set, so an
// expired session surfaces as a 401 and the ACCT-015 redirect fires instead of
// the caller being silently downgraded to the anonymous identity.
export function setAuthed(on: boolean): void {
  if (typeof window === "undefined") return;
  try {
    if (on) window.localStorage.setItem(AUTHED_KEY, "1");
    else window.localStorage.removeItem(AUTHED_KEY);
  } catch {
    // Storage disabled: nothing to persist; isAuthed() then reads false and the
    // anon token is sent, which a valid session cookie still overrides server-side.
  }
}

function isAuthed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(AUTHED_KEY) === "1";
  } catch {
    return false;
  }
}

// getAnonToken returns this browser's anonymous-draft token, minting and
// persisting one on first use. It is sent as X-Anon-Token on every request so a
// visitor with no session still owns the decks they build; on sign-in the token
// is handed to POST /api/auth/claim-drafts (ACCT-020, ACCT-021).
//
// @spec ACCT-020
export function getAnonToken(): string {
  if (typeof window === "undefined") return "";
  try {
    let tok = window.localStorage.getItem(ANON_TOKEN_KEY);
    if (!tok) {
      tok = crypto.randomUUID();
      window.localStorage.setItem(ANON_TOKEN_KEY, tok);
    }
    return tok;
  } catch {
    // Private-mode / storage-disabled: fall back to a per-tab token so the
    // session at least holds within one page's lifetime.
    return "";
  }
}

// A 401 means the session is missing or expired server-side. Redirect once,
// here, so pages carry no per-page logged-out logic (ACCT-015). M1 runs on the
// DevAuth stub so this rarely fires, but the contract is in place for M3.
//
// @spec ACCT-015
function handleUnauthorized(status: number): boolean {
  if (status !== 401) return false;
  setAuthed(false);
  if (typeof window !== "undefined") {
    // A hard navigation is deliberate: this is a plain utility module with no
    // router context, and a full reload also clears client state tied to the
    // now-invalid session.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.href = "/";
  }
  return true;
}

type RequestOptions = {
  // When true, a 401 is returned to the caller as an ApiError instead of
  // triggering the redirect-to-"/" session-expiry handler. The /api/auth/*
  // endpoints set this: a wrong password is a normal outcome there, not a
  // dead session.
  isAuthEndpoint?: boolean;
};

async function request<T>(path: string, init?: RequestInit, opts?: RequestOptions): Promise<T> {
  const anon = isAuthed() ? "" : getAnonToken();
  const res = await fetch(path, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(anon ? { "X-Anon-Token": anon } : {}),
      ...init?.headers,
    },
  });

  if (!opts?.isAuthEndpoint && handleUnauthorized(res.status)) {
    throw new ApiError(res.status, "Not authenticated");
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new ApiError(res.status, body.error ?? `Request failed (${res.status})`);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return res.json() as Promise<T>;
}

// ---- card-data ---------------------------------------------------------

export type CardSummary = {
  id: string;
  name: string;
  mana_cost: string | null;
  mana_value: number;
  type_line: string;
  oracle_text: string;
  colors: string[];
  color_identity: string[];
  keywords: string[];
  can_be_commander: boolean;
  edhrec_rank: number | null;
  layout: string;
  image_uris: { small?: string; normal?: string; large?: string } | null;
  prices: { usd?: string; eur?: string } | null;
  set_code: string;
  collector_number: string;
};

export type CardSearchResult = {
  cards: CardSummary[];
  total: number;
  page: number;
  has_more: boolean;
};

// ---- deck-building ---------------------------------------------------

export type Deck = {
  id: string;
  name: string;
  description: string;
  format: string;
  bracket: number | null;
  // A deck-template key from GET /api/deck-templates, or null (DECK-103).
  archetype: string | null;
  is_public: boolean;
  color_identity: string[];
};

export type DeckEntry = {
  entry_id: string;
  card_id: string;
  name: string;
  mana_cost: string | null;
  mana_value: number;
  type_line: string;
  color_identity: string[];
  quantity: number;
  board: string;
  category: string | null;
  image_uris: { small?: string; normal?: string } | null;
  prices: { usd?: string; eur?: string } | null;
  set_code: string;
  collector_number: string;
  color_identity_violation: boolean;
  offending_colors: string[];
  singleton_violation: boolean;
};

export type DeckDetail = Deck & {
  commander: CardSummary | null;
  partner: CardSummary | null;
  boards: Record<"command" | "main" | "maybe" | "sideboard", DeckEntry[]>;
};

export type ValidationReport = {
  color_identity_violations: { card_id: string; card_name: string; offending: string[] }[];
  singleton_violations: { card_id: string; card_name: string; quantity: number; limit: number }[];
  banlist_violations: { card_id: string; card_name: string; reason: string }[];
  main_command_count: number;
  count_deviation: number;
  commander_issues: string[];
  legal: boolean;
};

export type AddCardFlag = {
  entry_id: string;
  card_id: string;
  board: string;
  quantity: number;
  color_identity_violation: boolean;
  offending_colors: string[];
  singleton_violation: boolean;
};

// ---- import / export -------------------------------------------------

export type ImportFormat = "plaintext" | "mtga" | "moxfield" | "archidekt";

export type ResolvedLine = {
  card_id: string;
  name: string;
  quantity: number;
  board: string;
  category?: string;
};

export type UnresolvedLine = {
  name: string;
  quantity: number;
  board: string;
  raw: string;
};

// The parse step's result: what resolved against the mirror, what did not
// (never dropped — PORT-004), and lines the grammar could not read at all.
export type ImportPreview = {
  import_id: string;
  resolved: ResolvedLine[];
  unresolved: UnresolvedLine[];
  rejected: string[];
};

// ---- account-access ------------------------------------------------

export type SessionState = {
  authenticated: boolean;
  email?: string;
};

// ---- synergy -----------------------------------------------------------

// Where a commander's synergy data stands: gathered and fresh, being gathered,
// or neither. `reason` explains a `none` the caller can act on.
export type SynergyStatus = {
  status: "none" | "gathering" | "ready";
  reason: "disabled" | "daily_cap" | "not_a_commander" | null;
  sample: { deck_count: number; fetched_at: string } | null;
};

// ---- ai-assist ---------------------------------------------------

// One surviving suggestion: a real card from the mirror that passed the
// anti-hallucination gate, plus the model's one-sentence rationale.
export type AISuggestion = {
  card: CardSummary;
  reason: string;
};

export type AISuggestResponse = {
  suggestions: AISuggestion[];
  // How many model-named cards the gate dropped (nonexistent / banned / off
  // colour / already in the deck). Nothing is substituted for them.
  dropped: number;
  model: string;
};

export type AIExplainResponse = {
  card_id: string;
  explanation: string;
  model: string;
};

// ---- deck stats ----------------------------------------------------

export type DeckStats = {
  type_counts: Record<string, number>;
  avg_mana_value: number;
  mana_curve: Record<string, number>;
  color_pips: Record<string, number>;
  color_sources: Record<string, number>;
  category_counts: Record<string, number>;
  land_count: number;
  nonland_count: number;
  category_targets: Record<string, [number, number]>;
};

// ---- deck templates ----------------------------------------------------

// One row of a deck template: an inclusive target range for a card type or a
// functional role, out of the 99 non-commander cards (DECK-100).
export type TemplateRow = {
  key: string;
  label: string;
  kind: "type" | "role";
  min: number;
  max: number;
  // The one bracket at which a count below min, down to relaxed_floor, is
  // acceptable (the land row only: Bracket 4, 32).
  relaxed_at_bracket?: number;
  relaxed_floor?: number;
};

export type DeckTemplate = {
  key: string;
  name: string;
  rows: TemplateRow[];
};

export type DeckTemplates = {
  version: number;
  templates: DeckTemplate[];
};

export type FitStatus = "ok" | "under" | "over" | "relaxed";

export type TemplateRowFit = TemplateRow & { count: number; status: FitStatus };

// The deck's counts against its archetype's template (DECK-104); no rows when
// the deck has no archetype.
export type TemplateFit = {
  archetype: string | null;
  template_version: number;
  bracket: number | null;
  rows: TemplateRowFit[];
};

// ---- deck bracket check --------------------------------------------

export type BracketFinding = {
  severity: "violation" | "warning" | "notice";
  kind: "game_changers" | "combo" | "mass_land_denial" | "extra_turn";
  message: string;
  combo_id?: string;
  cards?: string[];
};

export type BracketCombo = {
  id: string;
  cards: string[];
  produces: string[];
  mana_needed: string;
  speed: number;
  relevant: boolean;
  definitely_two_card: boolean;
  arguably_two_card: boolean;
  tag: string;
  tag_label: string;
  min_bracket: number;
  borderline: boolean;
};

// ---- deck-wizard ------------------------------------------------------

export type WizardStep = "fixing" | "utility" | "fastmana" | "picks";

export type WizardPlan = {
  ready: boolean;
  reason: "no_commander" | "no_bracket" | "bracket_unsupported" | null;
  ai_enabled: boolean;
  bracket: number | null;
  archetype: string | null;
  policy: {
    game_changer_limit: number | null;
    game_changers_in_deck: number;
    game_changers_left: number | null;
    guidance: string;
  } | null;
  lands: {
    target: number;
    base_target: number;
    in_deck: number;
    basics_needed: number;
    fixing_applicable: boolean;
  };
  fast_mana: { applicable: boolean; in_deck: number };
  synergy: { status: "none" | "gathering" | "ready"; reason: string | null } | null;
};

export type WizardRestrictions = {
  no_tutors?: boolean;
  max_price_usd?: number;
  notes?: string;
};

export type WizardProposeBody = {
  step: WizardStep;
  include_fetch?: boolean;
  land_target?: number;
  restrictions?: WizardRestrictions;
  rejected_card_ids?: string[];
};

export type WizardProposal = {
  card: CardSummary;
  reason: string;
  game_changer: boolean;
  price_usd: number | null;
  synergy: { decks: number; inclusion: number; synergy: number | null } | null;
};

export type WizardGameChangers = { limit: number | null; in_deck: number; left: number | null };

export type WizardEmptyReason = "land_target_met" | "no_fixing_needed" | "deck_full" | "no_candidates";

export type WizardProposeResponse = {
  step: WizardStep;
  summary: string;
  source: "synergy" | "popular" | "lands" | "fast_mana";
  proposals: WizardProposal[];
  dropped: number;
  pool_size: number;
  empty_reason: WizardEmptyReason | null;
  game_changers: WizardGameChangers;
  combos_checked: boolean;
  synergy: {
    status: "none" | "gathering" | "ready";
    sample: { deck_count: number; fetched_at: string } | null;
    attribution: { text: string; url: string };
  } | null;
  model: string;
};

// The wizard's saved answers (WIZ-092): what the first slice asks, per deck.
export type WizardSavedState = {
  step: "setup" | "fixing" | "utility" | "picks" | "basics";
  restrictions: WizardRestrictions;
  fixing: "include" | "basics";
  include_fetch: boolean;
  utility: boolean;
  land_target: number | null;
  rejected_card_ids: string[];
};

export type WizardStateResponse =
  | { saved: false; state: null; updated_at: null }
  | { saved: true; state: WizardSavedState; updated_at: string };

export type WizardBasicsRow = { card: CardSummary; quantity: number; in_deck: number };

export type WizardBasics = {
  land_target: number;
  lands_in_deck: number;
  basics_needed: number;
  rows: WizardBasicsRow[];
  missing: string[];
};

export type BracketReport = {
  tag: string;
  tag_label: string;
  estimated_bracket: number;
  borderline: boolean;
  target_bracket: number | null;
  game_changers: { count: number; limit: number | null; cards: string[] };
  mass_land_denial: string[];
  extra_turns: string[];
  combos: BracketCombo[];
  findings: BracketFinding[];
  combo_data_available: boolean;
};

export const api = {
  // ---- account-access ----
  register: async (email: string, password: string) => {
    const state = await request<SessionState>(
      "/api/auth/register",
      { method: "POST", body: JSON.stringify({ email, password }) },
      { isAuthEndpoint: true },
    );
    setAuthed(state.authenticated);
    return state;
  },
  login: async (email: string, password: string) => {
    const state = await request<SessionState>(
      "/api/auth/login",
      { method: "POST", body: JSON.stringify({ email, password }) },
      { isAuthEndpoint: true },
    );
    setAuthed(state.authenticated);
    return state;
  },
  logout: async () => {
    try {
      await request<void>("/api/auth/logout", { method: "POST" }, { isAuthEndpoint: true });
    } finally {
      setAuthed(false);
    }
  },
  getSession: async () => {
    const state = await request<SessionState>("/api/auth/session", undefined, {
      isAuthEndpoint: true,
    });
    setAuthed(state.authenticated);
    return state;
  },
  claimDrafts: (anonToken: string) =>
    request<{ claimed: number }>(
      "/api/auth/claim-drafts",
      { method: "POST", body: JSON.stringify({ anon_token: anonToken }) },
      { isAuthEndpoint: true },
    ),

  searchCards: (q: string, page = 0) =>
    request<CardSearchResult>(`/api/cards/search?q=${encodeURIComponent(q)}&page=${page}`),
  autocompleteCards: (q: string) =>
    request<{ names: string[] }>(`/api/cards/autocomplete?q=${encodeURIComponent(q)}`),

  listDecks: () => request<{ decks: Deck[] }>("/api/decks"),
  createDeck: (name: string) =>
    request<Deck>("/api/decks", { method: "POST", body: JSON.stringify({ name }) }),
  getDeck: (id: string) => request<DeckDetail>(`/api/decks/${id}`),
  getPublicDeck: (id: string) => request<DeckDetail>(`/public/decks/${id}`),
  // archetype: a template key sets it, "" clears it (DECK-103).
  updateDeck: (
    id: string,
    patch: Partial<Pick<Deck, "name" | "description" | "is_public" | "bracket">> & {
      archetype?: string;
    },
  ) =>
    request<Deck>(`/api/decks/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  setCommander: (id: string, commanderCardId: string, partnerCardId?: string) =>
    request<DeckDetail>(`/api/decks/${id}/commander`, {
      method: "PUT",
      body: JSON.stringify({ commander_card_id: commanderCardId, partner_card_id: partnerCardId }),
    }),
  addCard: (id: string, cardId: string, board = "main", category?: string) =>
    request<AddCardFlag>(`/api/decks/${id}/cards`, {
      method: "POST",
      body: JSON.stringify({ card_id: cardId, board, category }),
    }),
  removeCard: (id: string, cardId: string, board = "main") =>
    request<void>(
      `/api/decks/${id}/cards/${cardId}?board=${encodeURIComponent(board)}`,
      { method: "DELETE" },
    ),
  // Set an existing entry's quantity in place; a quantity of 0 removes the
  // entry. Ownership-scoped exactly like add/remove (DECK-012).
  setCardQuantity: (id: string, cardId: string, board: string, quantity: number) =>
    request<void>(`/api/decks/${id}/cards/${cardId}`, {
      method: "PATCH",
      body: JSON.stringify({ board, quantity }),
    }),
  // Move an entry to another board, carrying its quantity / printing / category;
  // merges into any existing entry for the same card on the target board
  // (DECK-013).
  moveCard: (id: string, cardId: string, board: string, toBoard: string) =>
    request<void>(`/api/decks/${id}/cards/${cardId}`, {
      method: "PATCH",
      body: JSON.stringify({ board, to_board: toBoard }),
    }),
  getValidation: (id: string) => request<ValidationReport>(`/api/decks/${id}/validation`),
  getDeckStats: (id: string) => request<DeckStats>(`/api/decks/${id}/stats`),
  getDeckTemplates: () => request<DeckTemplates>("/api/deck-templates"),
  getTemplateFit: (id: string) => request<TemplateFit>(`/api/decks/${id}/template-fit`),
  getDeckBracket: (id: string) => request<BracketReport>(`/api/decks/${id}/bracket`),

  // ---- synergy ----
  // Queues gathering for a commander with no fresh synergy data and answers at
  // once; it never waits on Archidekt (SYN-030).
  ensureCommanderSynergy: (cardId: string) =>
    request<SynergyStatus>(`/api/synergy/commanders/${cardId}/ensure`, { method: "POST" }),

  // ---- deck-wizard ----
  getWizardPlan: (id: string) => request<WizardPlan>(`/api/decks/${id}/wizard`),
  wizardPropose: (id: string, body: WizardProposeBody) =>
    request<WizardProposeResponse>(`/api/decks/${id}/wizard/propose`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  // The wizard's saved answers, replaced whole on save and forgotten on reset
  // (WIZ-100, WIZ-101, WIZ-103).
  getWizardState: (id: string) => request<WizardStateResponse>(`/api/decks/${id}/wizard/state`),
  saveWizardState: (id: string, state: WizardSavedState) =>
    request<WizardStateResponse>(`/api/decks/${id}/wizard/state`, {
      method: "PUT",
      body: JSON.stringify(state),
    }),
  resetWizardState: (id: string) =>
    request<void>(`/api/decks/${id}/wizard/state`, { method: "DELETE" }),
  // Plans the basic lands the deck still needs; writes nothing (WIZ-053).
  wizardBasics: (id: string, landTarget?: number) =>
    request<WizardBasics>(`/api/decks/${id}/wizard/basics`, {
      method: "POST",
      body: JSON.stringify(landTarget === undefined ? {} : { land_target: landTarget }),
    }),

  // ---- ai-assist ----
  suggestDeck: (id: string) =>
    request<AISuggestResponse>(`/api/decks/${id}/suggestions`, { method: "POST" }),
  explainCard: (id: string, cardId: string) =>
    request<AIExplainResponse>(`/api/decks/${id}/cards/${cardId}/explain`, { method: "POST" }),

  parseImport: (id: string, sourceFormat: ImportFormat, rawText: string) =>
    request<ImportPreview>(`/api/decks/${id}/import`, {
      method: "POST",
      body: JSON.stringify({ source_format: sourceFormat, raw_text: rawText }),
    }),
  applyImport: (id: string, importId: string) =>
    request<DeckDetail>(`/api/decks/${id}/import/${importId}/apply`, { method: "POST" }),

  // Export returns text/plain, not JSON, so it bypasses the shared request()
  // helper and its JSON Content-Type / body handling.
  exportDeck: async (id: string, format: "plaintext" | "mtga"): Promise<string> => {
    const anon = isAuthed() ? "" : getAnonToken();
    const res = await fetch(`/api/decks/${id}/export?format=${format}`, {
      credentials: "include",
      headers: anon ? { "X-Anon-Token": anon } : {},
    });
    if (handleUnauthorized(res.status)) {
      throw new ApiError(res.status, "Not authenticated");
    }
    if (!res.ok) {
      throw new ApiError(res.status, `Export failed (${res.status})`);
    }
    return res.text();
  },
};
