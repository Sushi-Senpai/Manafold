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

// A 401 means the session is missing or expired server-side. Redirect once,
// here, so pages carry no per-page logged-out logic (ACCT-015). M1 runs on the
// DevAuth stub so this rarely fires, but the contract is in place for M3.
//
// @spec ACCT-015
function handleUnauthorized(status: number): boolean {
  if (status !== 401) return false;
  if (typeof window !== "undefined") {
    // A hard navigation is deliberate: this is a plain utility module with no
    // router context, and a full reload also clears client state tied to the
    // now-invalid session.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.href = "/";
  }
  return true;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...init?.headers },
  });

  if (handleUnauthorized(res.status)) {
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

export const api = {
  searchCards: (q: string, page = 0) =>
    request<CardSearchResult>(`/api/cards/search?q=${encodeURIComponent(q)}&page=${page}`),
  autocompleteCards: (q: string) =>
    request<{ names: string[] }>(`/api/cards/autocomplete?q=${encodeURIComponent(q)}`),

  listDecks: () => request<{ decks: Deck[] }>("/api/decks"),
  createDeck: (name: string) =>
    request<Deck>("/api/decks", { method: "POST", body: JSON.stringify({ name }) }),
  getDeck: (id: string) => request<DeckDetail>(`/api/decks/${id}`),
  getPublicDeck: (id: string) => request<DeckDetail>(`/api/public/decks/${id}`),
  updateDeck: (id: string, patch: Partial<Pick<Deck, "name" | "description" | "is_public" | "bracket">>) =>
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
  removeCard: (id: string, cardId: string) =>
    request<void>(`/api/decks/${id}/cards/${cardId}`, { method: "DELETE" }),
  getValidation: (id: string) => request<ValidationReport>(`/api/decks/${id}/validation`),
};
