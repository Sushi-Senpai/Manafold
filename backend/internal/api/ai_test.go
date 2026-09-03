package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/ai"
	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
)

// fakeAssistant is a scripted ai.Assistant: no network, the test supplies each
// method's behaviour.
type fakeAssistant struct {
	enabled bool
	suggest func(ai.SuggestRequest) (ai.SuggestResult, error)
	explain func(ai.ExplainRequest) (ai.ExplainResult, error)
}

func (f fakeAssistant) Enabled() bool { return f.enabled }

func (f fakeAssistant) Suggest(_ context.Context, r ai.SuggestRequest) (ai.SuggestResult, error) {
	return f.suggest(r)
}

func (f fakeAssistant) Explain(_ context.Context, r ai.ExplainRequest) (ai.ExplainResult, error) {
	return f.explain(r)
}

// aiServe runs one request against the deck + AI routes with an injected
// identity: a valid userID authenticates the caller, a zero userID plus a
// non-empty anonToken makes them an anonymous draft.
func aiServe(t *testing.T, a *API, userID pgtype.UUID, anonToken, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if userID.Valid {
					ctx = authctx.WithUserID(ctx, userID)
				} else if anonToken != "" {
					ctx = authctx.WithAnonToken(ctx, anonToken)
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		a.RegisterDeckRoutes(r)
		a.RegisterAIRoutes(r)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newJSONRequest(t, method, path, body))
	return rec
}

func newJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	if body == nil {
		return httptest.NewRequest(method, path, nil)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return httptest.NewRequest(method, path, bytes.NewReader(raw))
}

// makeBannedCard upserts a card whose Scryfall legalities mark it banned in
// Commander, for the anti-hallucination gate test.
func makeBannedCard(t *testing.T, a *API, name string, identity []string) pgtype.UUID {
	t.Helper()
	c, err := a.Queries.UpsertCard(context.Background(), db.UpsertCardParams{
		ScryfallOracleID:       randUUID(t),
		Name:                   name,
		ManaValue:              2,
		TypeLine:               "Artifact",
		OracleText:             "",
		Colors:                 identity,
		ColorIdentity:          identity,
		ProducedMana:           []string{},
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"banned"}`),
		Layout:                 "normal",
		CanBeCommander:         false,
		CommanderColorIdentity: identity,
	})
	if err != nil {
		t.Fatalf("upsert banned card %q: %v", name, err)
	}
	return c.ID
}

// setupDeckWithCommander creates a deck owned by owner with a {W}-identity
// commander assigned, and returns the deck id.
func setupDeckWithCommander(t *testing.T, a *API, owner pgtype.UUID) string {
	t.Helper()
	commander := makeCard(t, a, "AI Test Commander "+hex.EncodeToString(randBytes(t, 4)), "Legendary Creature — Human", []string{"W"}, true)

	rec := serve(t, a, owner, http.MethodPost, "/decks", map[string]string{"name": "AI Test Deck"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create deck: %d %s", rec.Code, rec.Body.String())
	}
	deckID := decode[deckJSON](t, rec).ID

	rec = serve(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", map[string]string{"commander_card_id": uuidString(commander)})
	if rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	return deckID
}

type suggestResponse struct {
	Suggestions []struct {
		Card   cardSummary `json:"card"`
		Reason string      `json:"reason"`
	} `json:"suggestions"`
	Dropped int    `json:"dropped"`
	Model   string `json:"model"`
}

// @spec AI-010, AI-013, AI-020
func TestSuggest_GateDropsIllegalAndDuplicateCards(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)

	suffix := hex.EncodeToString(randBytes(t, 4))
	good := "AI Good Card " + suffix
	makeCard(t, a, good, "Creature — Soldier", []string{"W"}, false)
	outOfIdentity := "AI Off-Colour Card " + suffix
	makeCard(t, a, outOfIdentity, "Creature — Horror", []string{"B"}, false)
	banned := "AI Banned Card " + suffix
	makeBannedCard(t, a, banned, []string{"W"})
	already := "AI Already Card " + suffix
	alreadyID := makeCard(t, a, already, "Creature — Bird", []string{"W"}, false)

	// Put the "already" card in the deck.
	if rec := serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(alreadyID), "board": "main"}); rec.Code != http.StatusCreated {
		t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
	}

	a.AI = fakeAssistant{
		enabled: true,
		suggest: func(ai.SuggestRequest) (ai.SuggestResult, error) {
			return ai.SuggestResult{
				Suggestions: []ai.Suggestion{
					{Name: good, Reason: "on-colour and useful"},
					{Name: outOfIdentity, Reason: "off colour, must be dropped"},
					{Name: banned, Reason: "banned, must be dropped"},
					{Name: already, Reason: "already in the deck, must be dropped"},
					{Name: "Nonexistent Card " + suffix, Reason: "not in the mirror, must be dropped"},
				},
				Usage: ai.Usage{Model: "claude-sonnet-5", InputTokens: 100, OutputTokens: 50, CostMicros: 700},
			}, nil
		},
	}
	a.AISuggestDailyLimit = 20

	rec := aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("suggestions: %d %s", rec.Code, rec.Body.String())
	}
	var resp suggestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("kept %d suggestions, want 1: %+v", len(resp.Suggestions), resp.Suggestions)
	}
	if resp.Suggestions[0].Card.Name != good {
		t.Fatalf("kept %q, want %q", resp.Suggestions[0].Card.Name, good)
	}
	if resp.Dropped != 4 {
		t.Fatalf("dropped = %d, want 4", resp.Dropped)
	}

	// AI-030 / AI-034: a successful call is metered.
	used, err := a.Queries.CountAIFeatureCallsToday(context.Background(), db.CountAIFeatureCallsTodayParams{UserID: owner, Feature: "suggest"})
	if err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if used != 1 {
		t.Fatalf("recorded %d suggest calls, want 1", used)
	}
}

// @spec AI-033
func TestSuggest_AnonymousForbidden(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)
	a.AI = fakeAssistant{enabled: true, suggest: func(ai.SuggestRequest) (ai.SuggestResult, error) {
		t.Fatal("assistant must not be called for an anonymous caller")
		return ai.SuggestResult{}, nil
	}}
	a.AISuggestDailyLimit = 20

	rec := aiServe(t, a, pgtype.UUID{}, "anon-token-abc", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous suggest: %d %s, want 403", rec.Code, rec.Body.String())
	}
}

// @spec AI-024
func TestSuggest_NoCommander(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	rec := serve(t, a, owner, http.MethodPost, "/decks", map[string]string{"name": "no commander"})
	deckID := decode[deckJSON](t, rec).ID

	a.AI = fakeAssistant{enabled: true, suggest: func(ai.SuggestRequest) (ai.SuggestResult, error) {
		t.Fatal("assistant must not be called when the deck has no commander")
		return ai.SuggestResult{}, nil
	}}
	a.AISuggestDailyLimit = 20

	rec = aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("suggest with no commander: %d %s, want 422", rec.Code, rec.Body.String())
	}
}

// @spec AI-001
func TestSuggest_DisabledAssistant(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)
	a.AI = ai.Disabled()
	a.AISuggestDailyLimit = 20

	rec := aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled suggest: %d %s, want 503", rec.Code, rec.Body.String())
	}
}

// @spec AI-031
func TestSuggest_DailyLimitReached(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)

	// Pre-record one call, then set the limit to 1.
	if err := a.Queries.RecordAIUsage(context.Background(), db.RecordAIUsageParams{UserID: owner, Feature: "suggest", InputTokens: 10, OutputTokens: 5, CostMicros: 100}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	a.AI = fakeAssistant{enabled: true, suggest: func(ai.SuggestRequest) (ai.SuggestResult, error) {
		t.Fatal("assistant must not be called once the daily limit is reached")
		return ai.SuggestResult{}, nil
	}}
	a.AISuggestDailyLimit = 1

	rec := aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit suggest: %d %s, want 429", rec.Code, rec.Body.String())
	}
}

// @spec AI-032
func TestSuggest_GlobalSpendCeiling(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)

	if err := a.Queries.RecordAIUsage(context.Background(), db.RecordAIUsageParams{UserID: owner, Feature: "suggest", InputTokens: 1, OutputTokens: 1, CostMicros: 5_000_000}); err != nil {
		t.Fatalf("seed spend: %v", err)
	}
	a.AI = fakeAssistant{enabled: true, suggest: func(ai.SuggestRequest) (ai.SuggestResult, error) {
		t.Fatal("assistant must not be called once the global ceiling is hit")
		return ai.SuggestResult{}, nil
	}}
	a.AISuggestDailyLimit = 20
	a.AIMonthlySpendMicros = 1 // any recorded spend trips it

	rec := aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("over-ceiling suggest: %d %s, want 503", rec.Code, rec.Body.String())
	}
}

// @spec AI-034
func TestSuggest_ProviderErrorIsNotMetered(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)

	a.AI = fakeAssistant{enabled: true, suggest: func(ai.SuggestRequest) (ai.SuggestResult, error) {
		return ai.SuggestResult{}, errors.New("provider exploded")
	}}
	a.AISuggestDailyLimit = 20

	rec := aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/suggestions", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("provider error: %d %s, want 502", rec.Code, rec.Body.String())
	}
	used, err := a.Queries.CountAIFeatureCallsToday(context.Background(), db.CountAIFeatureCallsTodayParams{UserID: owner, Feature: "suggest"})
	if err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if used != 0 {
		t.Fatalf("a failed call was metered: recorded %d, want 0", used)
	}
}

// @spec AI-021
func TestExplain_CardMustBeInDeck(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := setupDeckWithCommander(t, a, owner)

	suffix := hex.EncodeToString(randBytes(t, 4))
	inDeckID := makeCard(t, a, "AI Explain In "+suffix, "Creature — Soldier", []string{"W"}, false)
	if rec := serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(inDeckID), "board": "main"}); rec.Code != http.StatusCreated {
		t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
	}
	outsideID := makeCard(t, a, "AI Explain Out "+suffix, "Creature — Soldier", []string{"W"}, false)

	a.AI = fakeAssistant{enabled: true, explain: func(r ai.ExplainRequest) (ai.ExplainResult, error) {
		return ai.ExplainResult{
			Text:  "It fits because " + r.CardName + " supports the commander's plan.",
			Usage: ai.Usage{Model: "claude-haiku-4-5", InputTokens: 40, OutputTokens: 30, CostMicros: 190},
		}, nil
	}}
	a.AIExplainDailyLimit = 40

	// A card not on the deck's boards -> 404.
	rec := aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/cards/"+uuidString(outsideID)+"/explain", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("explain card not in deck: %d %s, want 404", rec.Code, rec.Body.String())
	}

	// A card on the main board -> 200 with prose.
	rec = aiServe(t, a, owner, "", http.MethodPost, "/decks/"+deckID+"/cards/"+uuidString(inDeckID)+"/explain", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("explain in-deck card: %d %s, want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Explanation string `json:"explanation"`
		Model       string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Explanation == "" {
		t.Fatal("explanation is empty")
	}
	if body.Model != "claude-haiku-4-5" {
		t.Fatalf("model = %q, want claude-haiku-4-5", body.Model)
	}
}

// @spec AI-011
func TestListSuggestionCandidates_PoolFilters(t *testing.T) {
	a := testAPI(t)
	ctx := context.Background()
	suffix := hex.EncodeToString(randBytes(t, 4))

	inPool := setEdhrecRank(t, a, makeCard(t, a, "Cand In "+suffix, "Creature", []string{"W"}, false), 100)
	offColour := setEdhrecRank(t, a, makeCard(t, a, "Cand Off "+suffix, "Creature", []string{"B"}, false), 101)
	bannedID := makeBannedCard(t, a, "Cand Banned "+suffix, []string{"W"})
	setEdhrecRank(t, a, bannedID, 102)
	excluded := setEdhrecRank(t, a, makeCard(t, a, "Cand Excluded "+suffix, "Creature", []string{"W"}, false), 103)
	noRank := makeCard(t, a, "Cand NoRank "+suffix, "Creature", []string{"W"}, false)

	got, err := a.Queries.ListSuggestionCandidates(ctx, db.ListSuggestionCandidatesParams{
		DeckIdentity: []string{"W"},
		ExcludeIds:   []pgtype.UUID{excluded},
		Lim:          50,
	})
	if err != nil {
		t.Fatalf("ListSuggestionCandidates: %v", err)
	}
	inResult := map[string]bool{}
	for _, c := range got {
		inResult[uuidString(c.ID)] = true
	}
	if !inResult[uuidString(inPool)] {
		t.Fatal("on-colour ranked non-banned card missing from the pool")
	}
	for name, id := range map[string]pgtype.UUID{
		"off-colour":     offColour,
		"banned":         bannedID,
		"excluded":       excluded,
		"no edhrec_rank": noRank,
	} {
		if inResult[uuidString(id)] {
			t.Fatalf("%s card leaked into the candidate pool", name)
		}
	}
}

// @spec AI-012
func TestMirrorSearch_FiltersIdentityAndBanned(t *testing.T) {
	a := testAPI(t)
	ctx := context.Background()
	suffix := hex.EncodeToString(randBytes(t, 4))

	onColour := setEdhrecRank(t, a, makeCard(t, a, "Mirror On "+suffix, "Creature — Bird", []string{"W"}, false), 200)
	offColour := setEdhrecRank(t, a, makeCard(t, a, "Mirror Off "+suffix, "Creature — Bird", []string{"B"}, false), 201)
	bannedID := makeBannedCard(t, a, "Mirror Banned "+suffix, []string{"W"})
	setEdhrecRank(t, a, bannedID, 202)
	// The banned fixture is an Artifact; give the search a type that matches all
	// three by searching on a shared oracle phrase instead.
	for _, id := range []pgtype.UUID{onColour, offColour, bannedID} {
		if _, err := a.Pool.Exec(ctx, "UPDATE cards SET oracle_text = $1 WHERE id = $2", "mirrortest "+suffix, id); err != nil {
			t.Fatalf("set oracle_text: %v", err)
		}
	}

	got, err := a.mirrorSearch([]string{"W"})(ctx, `o:"mirrortest `+suffix+`"`)
	if err != nil {
		t.Fatalf("mirrorSearch: %v", err)
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["Mirror On "+suffix] {
		t.Fatal("on-colour non-banned card missing from search_cards results")
	}
	if names["Mirror Off "+suffix] {
		t.Fatal("off-colour card leaked into search_cards results")
	}
	if names["Mirror Banned "+suffix] {
		t.Fatal("banned card leaked into search_cards results")
	}
}

// setEdhrecRank stamps an edhrec_rank on an existing card and returns its id.
func setEdhrecRank(t *testing.T, a *API, id pgtype.UUID, rank int32) pgtype.UUID {
	t.Helper()
	if _, err := a.Pool.Exec(context.Background(), "UPDATE cards SET edhrec_rank = $1 WHERE id = $2", rank, id); err != nil {
		t.Fatalf("set edhrec_rank: %v", err)
	}
	return id
}
