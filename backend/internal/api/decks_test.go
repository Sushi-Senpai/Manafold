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

	// DECK-010: the same card also sits on the maybeboard, so the deck now has
	// two entries for it — one on 'main', one on 'maybe'.
	rec = serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards",
		map[string]string{"card_id": uuidString(outOfIdentity), "board": "maybe"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add out-of-identity card to maybe: %d %s", rec.Code, rec.Body.String())
	}

	// DECK-010: removing the maybeboard entry is board-scoped — 204, and only
	// the maybeboard row goes; the mainboard copy survives.
	rec = serve(t, a, owner, http.MethodDelete, "/decks/"+deckID+"/cards/"+uuidString(outOfIdentity)+"?board=maybe", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove card from maybe = %d, want 204", rec.Code)
	}
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID, nil)
	detail = decode[deckDetailJSON](t, rec)
	if got := len(detail.Boards["maybe"]); got != 0 {
		t.Fatalf("maybe board has %d entries after board-scoped delete, want 0", got)
	}
	mainHas := false
	for _, e := range detail.Boards["main"] {
		if e.CardID == uuidString(outOfIdentity) {
			mainHas = true
		}
	}
	if !mainHas {
		t.Fatalf("board-scoped delete removed the mainboard copy: %+v", detail.Boards["main"])
	}

	// DECK-010: no board query param defaults to 'main' — 204, mainboard entry gone.
	rec = serve(t, a, owner, http.MethodDelete, "/decks/"+deckID+"/cards/"+uuidString(outOfIdentity), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove card from main = %d, want 204", rec.Code)
	}
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID, nil)
	detail = decode[deckDetailJSON](t, rec)
	for _, e := range detail.Boards["main"] {
		if e.CardID == uuidString(outOfIdentity) {
			t.Fatalf("default-board delete left the mainboard entry: %+v", detail.Boards["main"])
		}
	}

	// DECK-009: deleting again, with no matching entry, is indistinguishable
	// from a deck the caller does not own — the ownership-scoped DELETE matches
	// zero rows and the handler returns 404.
	rec = serve(t, a, owner, http.MethodDelete, "/decks/"+deckID+"/cards/"+uuidString(outOfIdentity), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("remove absent card = %d, want 404", rec.Code)
	}

	// DECK-009: a non-owner cannot remove a card — the DELETE is ownership-scoped
	// in the query, so it affects zero rows and the handler returns 404.
	rec = serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards",
		map[string]string{"card_id": uuidString(outOfIdentity)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("re-add card for non-owner delete test: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(t, a, other, http.MethodDelete, "/decks/"+deckID+"/cards/"+uuidString(outOfIdentity), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner remove card = %d, want 404", rec.Code)
	}
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID, nil)
	detail = decode[deckDetailJSON](t, rec)
	found := false
	for _, e := range detail.Boards["main"] {
		if e.CardID == uuidString(outOfIdentity) {
			found = true
		}
	}
	if !found {
		t.Fatalf("non-owner delete removed the owner's card: %+v", detail.Boards["main"])
	}
}

// @spec DECK-004
func TestAddCard_ViolationFlagScopedToCountedBoards(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)

	commander := makeCard(t, a, "Test Commander "+hex.EncodeToString(randBytes(t, 4)), "Legendary Creature — Human", []string{"W"}, true)
	outOfIdentity := makeCard(t, a, "Test Black Blob "+hex.EncodeToString(randBytes(t, 4)), "Creature — Horror", []string{"B"}, false)

	rec := serve(t, a, owner, http.MethodPost, "/decks", map[string]string{"name": "Board Scope Deck"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create deck: %d %s", rec.Code, rec.Body.String())
	}
	deckID := decode[deckJSON](t, rec).ID

	rec = serve(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", map[string]string{"commander_card_id": uuidString(commander)})
	if rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}

	// Staged on the maybeboard: not one of the boards DECK-004 / the validation
	// endpoint count, so the add response must not flag it.
	rec = serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards",
		map[string]string{"card_id": uuidString(outOfIdentity), "board": "maybe"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add to maybe: %d %s", rec.Code, rec.Body.String())
	}
	maybeFlag := decode[map[string]any](t, rec)
	if maybeFlag["color_identity_violation"] != false {
		t.Fatalf("out-of-identity card on maybe board flagged: %v", maybeFlag)
	}

	// The same card on the mainboard is flagged.
	rec = serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards",
		map[string]string{"card_id": uuidString(outOfIdentity), "board": "main"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add to main: %d %s", rec.Code, rec.Body.String())
	}
	mainFlag := decode[map[string]any](t, rec)
	if mainFlag["color_identity_violation"] != true {
		t.Fatalf("out-of-identity card on main board not flagged: %v", mainFlag)
	}

	// The add response and GET /validation agree: the violation is the single
	// main-board copy, not the maybeboard one.
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID+"/validation", nil)
	report := decode[struct {
		ColorIdentityViolations []struct {
			CardID string `json:"card_id"`
		} `json:"color_identity_violations"`
	}](t, rec)
	if len(report.ColorIdentityViolations) != 1 || report.ColorIdentityViolations[0].CardID != uuidString(outOfIdentity) {
		t.Fatalf("validation violations = %+v, want exactly the main-board copy", report.ColorIdentityViolations)
	}

	// GET /decks/{id} scopes the per-entry flag the same way: the same card on
	// the mainboard is flagged, the maybeboard copy is not.
	rec = serve(t, a, owner, http.MethodGet, "/decks/"+deckID, nil)
	detail := decode[deckDetailJSON](t, rec)
	if len(detail.Boards["main"]) != 1 || !detail.Boards["main"][0].ColorIdentityViolation {
		t.Fatalf("main-board entry not flagged: %+v", detail.Boards["main"])
	}
	if len(detail.Boards["maybe"]) != 1 || detail.Boards["maybe"][0].ColorIdentityViolation {
		t.Fatalf("maybeboard entry leaked the colour-identity flag: %+v", detail.Boards["maybe"])
	}
	if len(detail.Boards["maybe"][0].OffendingColors) != 0 {
		t.Fatalf("maybeboard entry carries offending_colors: %+v", detail.Boards["maybe"][0].OffendingColors)
	}
}

// entryQty returns the quantity of the given card on the given board of a
// freshly-fetched deck, or 0 when there is no such entry.
func entryQty(t *testing.T, a *API, owner pgtype.UUID, deckID, cardID, board string) int32 {
	t.Helper()
	rec := serve(t, a, owner, http.MethodGet, "/decks/"+deckID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get deck: %d %s", rec.Code, rec.Body.String())
	}
	for _, e := range decode[deckDetailJSON](t, rec).Boards[board] {
		if e.CardID == cardID {
			return e.Quantity
		}
	}
	return 0
}

// @spec DECK-012, DECK-013, DECK-009
func TestPatchCard_QuantityAndBoardMove(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	other := makeUser(t, a)

	commander := makeCard(t, a, "Patch Commander "+hex.EncodeToString(randBytes(t, 4)), "Legendary Creature — Human", []string{"G"}, true)
	card := makeCard(t, a, "Patch Forest "+hex.EncodeToString(randBytes(t, 4)), "Basic Land — Forest", []string{}, false)

	rec := serve(t, a, owner, http.MethodPost, "/decks", map[string]string{"name": "Patch Deck"})
	deckID := decode[deckJSON](t, rec).ID
	rec = serve(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", map[string]string{"commander_card_id": uuidString(commander)})
	if rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(card)}); rec.Code != http.StatusCreated {
		t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
	}

	// DECK-012: set the quantity in place.
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main", "quantity": 7})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("set quantity: %d %s, want 204", rec.Code, rec.Body.String())
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "main"); got != 7 {
		t.Fatalf("main quantity = %d, want 7", got)
	}

	// DECK-012: a negative quantity is rejected, the entry is untouched.
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main", "quantity": -1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative quantity = %d, want 400", rec.Code)
	}

	// DECK-012: a quantity past any real-deck bound is a clean 400, not an int32
	// wrap or a CHECK-constraint 500.
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main", "quantity": 1 << 40})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range quantity = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "main"); got != 7 {
		t.Fatalf("out-of-range patch changed the quantity: %d, want 7", got)
	}

	// DECK-013: move the entry from main to sideboard, quantity travels with it.
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main", "to_board": "sideboard"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("move card: %d %s, want 204", rec.Code, rec.Body.String())
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "main"); got != 0 {
		t.Fatalf("main still has the card after a move: qty %d", got)
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "sideboard"); got != 7 {
		t.Fatalf("sideboard quantity after move = %d, want 7", got)
	}

	// DECK-013: moving onto a board that already holds the card merges the
	// quantities rather than colliding on the unique key.
	if rec := serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(card), "board": "main"}); rec.Code != http.StatusCreated {
		t.Fatalf("re-add on main: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "sideboard", "to_board": "main"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("merge move: %d %s, want 204", rec.Code, rec.Body.String())
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "main"); got != 8 {
		t.Fatalf("merged main quantity = %d, want 8 (1 + 7)", got)
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "sideboard"); got != 0 {
		t.Fatalf("sideboard not emptied by the move: qty %d", got)
	}

	// DECK-012: quantity 0 deletes the entry.
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main", "quantity": 0})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("quantity 0: %d %s, want 204", rec.Code, rec.Body.String())
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "main"); got != 0 {
		t.Fatalf("entry survived a quantity-0 patch: qty %d", got)
	}

	// DECK-012: neither quantity nor to_board is a 400.
	rec = serve(t, a, owner, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty patch = %d, want 400", rec.Code)
	}

	// DECK-009: a non-owner's patch matches no row and returns 404.
	if rec := serve(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]string{"card_id": uuidString(card), "board": "main"}); rec.Code != http.StatusCreated {
		t.Fatalf("re-add for non-owner check: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(t, a, other, http.MethodPatch, "/decks/"+deckID+"/cards/"+uuidString(card), map[string]any{"board": "main", "quantity": 3})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner patch = %d, want 404", rec.Code)
	}
	if got := entryQty(t, a, owner, deckID, uuidString(card), "main"); got != 1 {
		t.Fatalf("non-owner patch changed the quantity: %d, want 1", got)
	}
}
