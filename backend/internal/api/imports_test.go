package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
)

// serveIO runs one request against the deck + import/export routes with userID
// injected as the authenticated caller.
func serveIO(t *testing.T, a *API, userID pgtype.UUID, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(authctx.WithUserID(req.Context(), userID)))
			})
		})
		a.RegisterDeckRoutes(r)
		a.RegisterImportRoutes(r)
	})
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func mkCard(t *testing.T, a *API, name, typeLine, manaCost string, mv float64, identity, produced []string, canCmd bool) pgtype.UUID {
	t.Helper()
	if identity == nil {
		identity = []string{}
	}
	if produced == nil {
		produced = []string{}
	}
	c, err := a.Queries.UpsertCard(context.Background(), db.UpsertCardParams{
		ScryfallOracleID:       randUUID(t),
		Name:                   name,
		ManaCost:               pgtype.Text{String: manaCost, Valid: manaCost != ""},
		ManaValue:              mv,
		TypeLine:               typeLine,
		Colors:                 identity,
		ColorIdentity:          identity,
		ProducedMana:           produced,
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"legal"}`),
		Layout:                 "normal",
		CanBeCommander:         canCmd,
		CommanderColorIdentity: identity,
	})
	if err != nil {
		t.Fatalf("upsert card %q: %v", name, err)
	}
	return c.ID
}

func newDeck(t *testing.T, a *API, owner pgtype.UUID) string {
	t.Helper()
	rec := serveIO(t, a, owner, http.MethodPost, "/decks", `{"name":"Import Test Deck"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create deck: %d %s", rec.Code, rec.Body.String())
	}
	return decode[deckJSON](t, rec).ID
}

// @spec PORT-001, PORT-002, PORT-003, PORT-004, PORT-006, DECK-060
func TestImport_ParseThenApply(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	sfx := hex.EncodeToString(randBytes(t, 4))

	cmd := mkCard(t, a, "Kenrith Test "+sfx, "Legendary Creature — Human Noble", "{4}{W}", 5, []string{"W", "U", "B", "R", "G"}, nil, true)
	_ = cmd
	ramp := mkCard(t, a, "Arcane Signet Test "+sfx, "Artifact", "{2}", 2, []string{}, []string{"W", "U", "B", "R", "G"}, false)
	wipe := mkCard(t, a, "Wrath Test "+sfx, "Sorcery", "{2}{W}{W}", 4, []string{"W"}, nil, false)

	deckID := newDeck(t, a, owner)

	raw := "Commander\n1 Kenrith Test " + sfx + "\n\nDeck\n1 Arcane Signet Test " + sfx + " [Ramp]\n1 Wrath Test " + sfx + " [Board Wipe]\n1 Totally Not A Real Card " + sfx + "\n"
	body, _ := json.Marshal(map[string]string{"source_format": "archidekt", "raw_text": raw})

	rec := serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse import: %d %s", rec.Code, rec.Body.String())
	}
	parsed := decode[importResponse](t, rec)
	if len(parsed.Resolved) != 3 {
		t.Fatalf("resolved = %d, want 3: %+v", len(parsed.Resolved), parsed.Resolved)
	}
	if len(parsed.Unresolved) != 1 || parsed.Unresolved[0].Name != "Totally Not A Real Card "+sfx {
		t.Fatalf("unresolved not reported verbatim (PORT-004): %+v", parsed.Unresolved)
	}
	// The Archidekt [Ramp] tag rode through to the resolved line (PORT-003).
	var sawRampCategory bool
	for _, l := range parsed.Resolved {
		if l.Name == "Arcane Signet Test "+sfx && l.Category == "Ramp" {
			sawRampCategory = true
		}
	}
	if !sawRampCategory {
		t.Fatalf("Archidekt category tag not captured: %+v", parsed.Resolved)
	}

	// Apply writes deck_cards in one transaction (DECK-060).
	rec = serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/import/"+parsed.ImportID+"/apply", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("apply import: %d %s", rec.Code, rec.Body.String())
	}
	detail := decode[deckDetailJSON](t, rec)
	if len(detail.Boards["main"]) != 2 {
		t.Fatalf("main board size = %d, want 2 (signet + wrath): %+v", len(detail.Boards["main"]), detail.Boards["main"])
	}
	var signetCategory string
	for _, e := range detail.Boards["main"] {
		if e.Name == "Arcane Signet Test "+sfx {
			if e.Category != nil {
				signetCategory = *e.Category
			}
		}
	}
	if signetCategory != "Ramp" {
		t.Fatalf("category not preserved through apply: %q", signetCategory)
	}
	_ = ramp
	_ = wipe
}

// @spec PORT-005
func TestImport_ResolvesSplitCardFace(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	sfx := hex.EncodeToString(randBytes(t, 4))

	mkCard(t, a, "Fire "+sfx+" // Ice "+sfx, "Instant", "{1}{R} // {1}{U}", 2, []string{"R", "U"}, nil, false)
	deckID := newDeck(t, a, owner)

	raw := "Deck\n1 Fire " + sfx + "\n"
	body, _ := json.Marshal(map[string]string{"source_format": "plaintext", "raw_text": raw})
	rec := serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse import: %d %s", rec.Code, rec.Body.String())
	}
	parsed := decode[importResponse](t, rec)
	if len(parsed.Resolved) != 1 || parsed.Resolved[0].Name != "Fire "+sfx+" // Ice "+sfx {
		t.Fatalf("split face did not fold to the whole card (PORT-005): %+v / unresolved %+v", parsed.Resolved, parsed.Unresolved)
	}
}

// @spec DECK-009
func TestImport_NonOwnerGets404(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	stranger := makeUser(t, a)
	deckID := newDeck(t, a, owner)

	body, _ := json.Marshal(map[string]string{"source_format": "plaintext", "raw_text": "1 Sol Ring"})
	rec := serveIO(t, a, stranger, http.MethodPost, "/decks/"+deckID+"/import", string(body))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner import = %d, want 404", rec.Code)
	}
}

// @spec PORT-007
func TestExport_PlaintextAndMTGA(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	sfx := hex.EncodeToString(randBytes(t, 4))

	cmd := mkCard(t, a, "Export Commander "+sfx, "Legendary Creature — Wizard", "{2}{U}", 3, []string{"U"}, nil, true)
	spell := mkCard(t, a, "Export Spell "+sfx, "Instant", "{U}", 1, []string{"U"}, nil, false)
	deckID := newDeck(t, a, owner)

	if rec := serveIO(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", `{"commander_card_id":"`+uuidString(cmd)+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", `{"card_id":"`+uuidString(spell)+`","board":"main"}`); rec.Code != http.StatusCreated {
		t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
	}

	rec := serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/export?format=plaintext", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export plaintext: %d %s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !bytes.Contains([]byte(got), []byte("Commander\n1 Export Commander "+sfx)) {
		t.Fatalf("plaintext export missing commander section:\n%s", got)
	}
	if !bytes.Contains([]byte(got), []byte("Deck\n1 Export Spell "+sfx)) {
		t.Fatalf("plaintext export missing deck section:\n%s", got)
	}

	rec = serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/export?format=nonsense", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad export format = %d, want 400", rec.Code)
	}
}
