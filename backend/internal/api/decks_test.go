package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
)

func testAPI(t *testing.T) *API {
	t.Helper()
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return &API{Pool: pool, Queries: db.New(pool)}
}

func randUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var u pgtype.UUID
	if err := u.Scan(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}

func makeUser(t *testing.T, a *API) pgtype.UUID {
	t.Helper()
	u, err := a.Queries.CreateUser(context.Background(), db.CreateUserParams{
		Email: "apitest-" + hex.EncodeToString(randBytes(t, 8)) + "@manafold.test",
		Name:  "API Test User",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

func makeCard(t *testing.T, a *API, name, typeLine string, identity []string, canBeCommander bool) pgtype.UUID {
	t.Helper()
	c, err := a.Queries.UpsertCard(context.Background(), db.UpsertCardParams{
		ScryfallOracleID:       randUUID(t),
		Name:                   name,
		ManaValue:              2,
		TypeLine:               typeLine,
		OracleText:             "",
		Colors:                 identity,
		ColorIdentity:          identity,
		ProducedMana:           []string{},
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"legal"}`),
		Layout:                 "normal",
		CanBeCommander:         canBeCommander,
		CommanderColorIdentity: identity,
	})
	if err != nil {
		t.Fatalf("upsert card %q: %v", name, err)
	}
	return c.ID
}

// serve runs one request against the deck routes with userID injected as the
// authenticated caller, mirroring what DevAuth/SessionAuth would attach.
func serve(t *testing.T, a *API, userID pgtype.UUID, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(authctx.WithUserID(req.Context(), userID)))
			})
		})
		a.RegisterDeckRoutes(r)
	})

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return v
}

// @spec DECK-001, DECK-002, DECK-003, DECK-004, DECK-008, DECK-009
func TestDeckEndpoints_EndToEnd(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	other := makeUser(t, a)

	commander := makeCard(t, a, "Test Commander "+hex.EncodeToString(randBytes(t, 4)), "Legendary Creature — Human", []string{"W"}, true)
	inIdentity := makeCard(t, a, "Test Plains Walker "+hex.EncodeToString(randBytes(t, 4)), "Creature — Soldier", []string{"W"}, false)
	outOfIdentity := makeCard(t, a, "Test Swamp Thing "+hex.EncodeToString(randBytes(t, 4)), "Creature — Horror", []string{"B"}, false)

	// DECK-001: create.
	rec := serve(t, a, owner, http.MethodPost, "/decks", map[string]string{"name": "My Commander Deck"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create deck: %d %s", rec.Code, rec.Body.String())
	}
	created := decode[deckJSON](t, rec)
	deckID := created.ID

	// DECK-002 / DECK-003: assign a legal commander, deck identity becomes {W}.
	rec = serve(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", map[string]string{"commander_card_id": uuidString(commander)})
	if rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	detail := decode[deckDetailJSON](t, rec)
	if len(detail.ColorIdentity) != 1 || detail.ColorIdentity[0] != "W" {
		t.Fatalf("deck color_identity = %v, want [W]", detail.ColorIdentity)
	}
	if detail.Commander == nil || detail.Commander.ID != uuidString(commander) {
		t.Fatalf("commander not reflected in detail: %+v", detail.Commander)
	}

	// DECK-002: a non-commander card is rejected 422, commander unchanged.
	rec = serve(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", map[string]string{"commander_card_id": uuidString(inIdentity)})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("set illegal commander: %d %s, want 422", rec.Code, rec.Body.String())
	}

	// DECK-004: add an in-identity card — no colour-identity flag.
	rec = serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(inIdentity)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add in-identity card: %d %s", rec.Code, rec.Body.String())
	}
	flag := decode[map[string]any](t, rec)
	if flag["color_identity_violation"] != false {
		t.Fatalf("in-identity card flagged as violation: %v", flag)
	}

	// DECK-004: add an out-of-identity card — recorded, and flagged.
	rec = serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(outOfIdentity)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add out-of-identity card: %d %s", rec.Code, rec.Body.String())
	}
	flag = decode[map[string]any](t, rec)
	if flag["color_identity_violation"] != true {
		t.Fatalf("out-of-identity card not flagged: %v", flag)
	}

	// DECK-008: validation report reflects the violation and the count.
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID+"/validation", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("validation: %d %s", rec.Code, rec.Body.String())
	}
	report := decode[struct {
		ColorIdentityViolations []any `json:"color_identity_violations"`
		MainCommandCount        int   `json:"main_command_count"`
		CountDeviation          int   `json:"count_deviation"`
		Legal                   bool  `json:"legal"`
	}](t, rec)
	if len(report.ColorIdentityViolations) != 1 {
		t.Fatalf("validation color_identity_violations = %d, want 1", len(report.ColorIdentityViolations))
	}
	if report.MainCommandCount != 3 { // commander + 2 main cards
		t.Fatalf("main_command_count = %d, want 3", report.MainCommandCount)
	}
	if report.CountDeviation != -97 || report.Legal {
		t.Fatalf("count_deviation = %d, legal = %v, want -97 / false", report.CountDeviation, report.Legal)
	}

	// DECK-009: a non-owner cannot read the deck.
	rec = serve(t, a, other, http.MethodGet, "/decks/"+deckID, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner GET deck = %d, want 404", rec.Code)
	}

	// DECK-009: a non-owner cannot add a card — the write is ownership-scoped
	// in the query, so it affects zero rows and the handler returns 404.
	rec = serve(t, a, other, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(inIdentity)})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner add card = %d %s, want 404", rec.Code, rec.Body.String())
	}

	// The non-owner's write must not have landed.
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID, nil)
	detail = decode[deckDetailJSON](t, rec)
	if got := len(detail.Boards["main"]); got != 2 {
		t.Fatalf("main board has %d entries after blocked write, want 2", got)
	}

	// DECK-010: owner removes an entry, 204.
	rec = serve(t, a, owner, http.MethodDelete, "/decks/"+deckID+"/cards/"+uuidString(outOfIdentity), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove card = %d, want 204", rec.Code)
	}
}
