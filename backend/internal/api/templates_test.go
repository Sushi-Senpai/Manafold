package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/decktemplates"
)

// mkOracleCard upserts a card carrying Oracle text (and optionally faces), so
// the role classifier has something to read.
func mkOracleCard(t *testing.T, a *API, name, typeLine, oracle string, faces string) pgtype.UUID {
	t.Helper()
	params := db.UpsertCardParams{
		ScryfallOracleID:       randUUID(t),
		Name:                   name,
		TypeLine:               typeLine,
		OracleText:             oracle,
		Colors:                 []string{},
		ColorIdentity:          []string{},
		ProducedMana:           []string{},
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"legal"}`),
		Layout:                 "normal",
		CommanderColorIdentity: []string{},
	}
	if faces != "" {
		params.CardFaces = []byte(faces)
		params.Layout = "modal_dfc"
	}
	c, err := a.Queries.UpsertCard(context.Background(), params)
	if err != nil {
		t.Fatalf("upsert card %q: %v", name, err)
	}
	return c.ID
}

func fitRow(t *testing.T, fit templateFitJSON, key string) decktemplates.RowFit {
	t.Helper()
	for _, r := range fit.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("template-fit has no %s row: %+v", key, fit.Rows)
	return decktemplates.RowFit{}
}

// @spec DECK-100
func TestListDeckTemplates_ServesAuthoritativeData(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	rec := serveIO(t, a, owner, http.MethodGet, "/deck-templates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list templates: %d %s", rec.Code, rec.Body.String())
	}
	got := decode[deckTemplatesJSON](t, rec)
	if got.Version != decktemplates.Version || len(got.Templates) != len(decktemplates.All()) {
		t.Fatalf("templates payload = v%d × %d, want v%d × %d", got.Version, len(got.Templates), decktemplates.Version, len(decktemplates.All()))
	}
	want, _ := decktemplates.Lookup("control")
	for _, tpl := range got.Templates {
		if tpl.Key == "control" && len(tpl.Rows) != len(want.Rows) {
			t.Fatalf("control rows = %d, want %d", len(tpl.Rows), len(want.Rows))
		}
	}
}

// @spec DECK-103, DECK-104, DECK-105, DECK-106, DECK-108
func TestTemplateFit_EndToEnd(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	sfx := hex.EncodeToString(randBytes(t, 4))

	cmd := mkCard(t, a, "Fit Commander "+sfx, "Legendary Creature — Elf", "{G}", 1, []string{"G"}, nil, true)
	forest := mkOracleCard(t, a, "Fit Forest "+sfx, "Basic Land — Forest", "({T}: Add {G}.)", "")
	dork := mkOracleCard(t, a, "Fit Dork "+sfx, "Creature — Elf Druid", "{T}: Add {G}.", "")
	golem := mkOracleCard(t, a, "Fit Golem "+sfx, "Artifact Creature — Golem", "When this creature dies, you may draw a card.", "")
	bolt := mkOracleCard(t, a, "Fit Bolt "+sfx, "Instant", "Fit Bolt deals 3 damage to any target.", "")
	wrath := mkOracleCard(t, a, "Fit Wrath "+sfx, "Sorcery", "Destroy all creatures. They can't be regenerated.", "")
	walker := mkOracleCard(t, a, "Fit Walker "+sfx, "Legendary Planeswalker — Test", "+1: Draw a card.", "")
	mdfc := mkOracleCard(t, a, "Fit Rebirth "+sfx, "Instant // Land", "",
		`[{"type_line":"Instant","oracle_text":"Destroy target creature."},{"type_line":"Land","oracle_text":"{T}: Add {B}."}]`)
	maybeRock := mkOracleCard(t, a, "Fit Maybe Rock "+sfx, "Artifact", "{T}: Add {C}.", "")

	deckID := newDeck(t, a, owner)

	// No archetype yet: no rows, null archetype.
	rec := serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/template-fit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("template-fit: %d %s", rec.Code, rec.Body.String())
	}
	if fit := decode[templateFitJSON](t, rec); fit.Archetype != nil || len(fit.Rows) != 0 {
		t.Fatalf("no-archetype fit = %+v, want null archetype and no rows", fit)
	}

	// An unknown archetype is 400 and leaves the deck unchanged.
	if rec := serveIO(t, a, owner, http.MethodPatch, "/decks/"+deckID, `{"archetype":"not_a_template"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown archetype = %d, want 400", rec.Code)
	}
	rec = serveIO(t, a, owner, http.MethodPatch, "/decks/"+deckID, `{"archetype":"control"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set archetype: %d %s", rec.Code, rec.Body.String())
	}
	if d := decode[deckJSON](t, rec); d.Archetype == nil || *d.Archetype != "control" {
		t.Fatalf("archetype = %v, want control", d.Archetype)
	}

	if rec := serveIO(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", `{"commander_card_id":"`+uuidString(cmd)+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	add := func(id pgtype.UUID, board string, qty int) {
		t.Helper()
		for i := 0; i < qty; i++ {
			body := `{"card_id":"` + uuidString(id) + `","board":"` + board + `"}`
			if rec := serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", body); rec.Code != http.StatusCreated {
				t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
			}
		}
	}
	add(forest, "main", 30)
	add(dork, "main", 1)
	add(golem, "main", 1)
	add(bolt, "main", 1)
	add(wrath, "main", 1)
	add(walker, "main", 1)
	add(mdfc, "main", 1)
	add(maybeRock, "maybe", 1) // staging boards are not counted

	rec = serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/template-fit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("template-fit: %d %s", rec.Code, rec.Body.String())
	}
	fit := decode[templateFitJSON](t, rec)
	if fit.Archetype == nil || *fit.Archetype != "control" || fit.TemplateVersion != decktemplates.Version {
		t.Fatalf("fit header = %v v%d", fit.Archetype, fit.TemplateVersion)
	}
	for key, want := range map[string]struct {
		count  int
		status decktemplates.Status
	}{
		// 30 Forests + the modal DFC (deckstats reads its whole type line).
		decktemplates.RowLands: {31, decktemplates.StatusUnder},
		// dork + artifact creature (the commander is not counted).
		decktemplates.RowCreatures:         {2, decktemplates.StatusUnder},
		decktemplates.RowInstantsSorceries: {2, decktemplates.StatusUnder},
		decktemplates.RowArtifacts:         {0, decktemplates.StatusUnder},
		decktemplates.RowRamp:              {1, decktemplates.StatusUnder},
		decktemplates.RowCardDraw:          {2, decktemplates.StatusUnder},
		// bolt + the DFC's front face.
		decktemplates.RowTargetedRemoval: {2, decktemplates.StatusUnder},
		decktemplates.RowBoardWipes:      {1, decktemplates.StatusUnder},
	} {
		if r := fitRow(t, fit, key); r.Count != want.count || r.Status != want.status {
			t.Errorf("%s = %d %s, want %d %s", key, r.Count, r.Status, want.count, want.status)
		}
	}

	// Bracket 4 relaxes the land floor only.
	if rec := serveIO(t, a, owner, http.MethodPatch, "/decks/"+deckID, `{"bracket":4}`); rec.Code != http.StatusOK {
		t.Fatalf("set bracket: %d %s", rec.Code, rec.Body.String())
	}
	fit = decode[templateFitJSON](t, serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/template-fit", ""))
	if r := fitRow(t, fit, decktemplates.RowLands); r.Status != decktemplates.StatusRelaxed {
		t.Errorf("bracket 4 lands = %s, want relaxed", r.Status)
	}
	if r := fitRow(t, fit, decktemplates.RowCreatures); r.Status != decktemplates.StatusUnder {
		t.Errorf("bracket 4 creatures = %s, want under", r.Status)
	}

	// "" clears the archetype.
	rec = serveIO(t, a, owner, http.MethodPatch, "/decks/"+deckID, `{"archetype":""}`)
	if d := decode[deckJSON](t, rec); rec.Code != http.StatusOK || d.Archetype != nil {
		t.Fatalf("clear archetype = %d %v", rec.Code, d.Archetype)
	}

	// Non-owner 404 (DECK-009).
	stranger := makeUser(t, a)
	if rec := serveIO(t, a, stranger, http.MethodGet, "/decks/"+deckID+"/template-fit", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner template-fit = %d, want 404", rec.Code)
	}
}
