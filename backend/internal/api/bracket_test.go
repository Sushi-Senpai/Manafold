package api

import (
	"context"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/bracket"
	db "manafold-backend/internal/db/generated"
)

// lockCombos serialises combo-dependent tests against cardsync's combo-pass
// tests, which replace the whole mirror (advisory key 7241 in both).
func lockCombos(t *testing.T, a *API) {
	t.Helper()
	ctx := context.Background()
	conn, err := a.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7241)`); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(7241)`)
		conn.Release()
	})
}

func oracleOf(t *testing.T, a *API, cardID pgtype.UUID) pgtype.UUID {
	t.Helper()
	var o pgtype.UUID
	if err := a.Pool.QueryRow(context.Background(), `SELECT scryfall_oracle_id FROM cards WHERE id = $1`, cardID).Scan(&o); err != nil {
		t.Fatalf("oracle id: %v", err)
	}
	return o
}

func addToDeck(t *testing.T, a *API, owner pgtype.UUID, deckID string, card pgtype.UUID, board string) {
	t.Helper()
	rec := serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", `{"card_id":"`+uuidString(card)+`","board":"`+board+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
	}
}

// @spec DECK-120, DECK-121, DECK-122, DECK-124, DECK-125, DECK-126
func TestDeckBracket_EndToEnd(t *testing.T) {
	a := testAPI(t)
	lockCombos(t, a)
	ctx := context.Background()
	owner := makeUser(t, a)
	sfx := hex.EncodeToString(randBytes(t, 4))

	cmd := mkCard(t, a, "Bracket Commander "+sfx, "Legendary Creature — Elf", "{1}{G}", 2, []string{"G"}, nil, true)
	tutor := mkCard(t, a, "Bracket Tutor "+sfx, "Sorcery", "{B}", 1, []string{"B"}, nil, false)
	pieceA := mkCard(t, a, "Bracket Piece A "+sfx, "Creature — Elf", "{G}", 1, []string{"G"}, nil, false)
	pieceB := mkCard(t, a, "Bracket Piece B "+sfx, "Creature — Elf", "{G}", 1, []string{"G"}, nil, false)
	outsider := mkCard(t, a, "Bracket Outsider "+sfx, "Creature — Elf", "{G}", 1, []string{"G"}, nil, false)
	armageddon := mkCard(t, a, "Bracket Armageddon "+sfx, "Sorcery", "{3}{W}", 4, []string{"W"}, nil, false)
	timeWarp := mkCard(t, a, "Bracket Time Warp "+sfx, "Sorcery", "{3}{U}{U}", 5, []string{"U"}, nil, false)

	if _, err := a.Pool.Exec(ctx, `UPDATE cards SET is_game_changer = true WHERE id = $1`, tutor); err != nil {
		t.Fatal(err)
	}
	oA, oB, oOut := oracleOf(t, a, pieceA), oracleOf(t, a, pieceB), oracleOf(t, a, outsider)
	oArm, oTW := oracleOf(t, a, armageddon), oracleOf(t, a, timeWarp)

	comboID, otherID := "test-"+sfx+"-1", "test-"+sfx+"-2"
	for _, id := range []string{comboID, otherID} {
		if _, err := a.Pool.Exec(ctx, `INSERT INTO combos (id, identity, mana_needed, mana_value_needed, has_notable_prerequisites, produces, bracket_tag)
			VALUES ($1, 'G', '{G}', 2, false, '[{"name":"Win the game","status":"S"}]', 'R')`, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, ic := range []struct {
		combo  string
		oracle pgtype.UUID
	}{{comboID, oA}, {comboID, oB}, {otherID, oA}, {otherID, oOut}} {
		if _, err := a.Pool.Exec(ctx, `INSERT INTO combo_cards (combo_id, scryfall_oracle_id, quantity, zone_locations, must_be_commander)
			VALUES ($1, $2, 1, 'H', false)`, ic.combo, ic.oracle); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []db.InsertSpellbookCardFlagParams{
		{ScryfallOracleID: oArm, CardName: "Armageddon", MassLandDenial: true},
		{ScryfallOracleID: oTW, CardName: "Time Warp", ExtraTurn: true},
	} {
		if err := a.Queries.InsertSpellbookCardFlag(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = a.Pool.Exec(ctx, `DELETE FROM combos WHERE id = ANY($1)`, []string{comboID, otherID})
		_, _ = a.Pool.Exec(ctx, `DELETE FROM spellbook_card_flags WHERE scryfall_oracle_id = ANY($1)`, []pgtype.UUID{oArm, oTW})
	})

	deckID := newDeck(t, a, owner)
	if rec := serveIO(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", `{"commander_card_id":"`+uuidString(cmd)+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []pgtype.UUID{tutor, pieceA, pieceB, armageddon, timeWarp} {
		addToDeck(t, a, owner, deckID, c, "main")
	}
	// A maybe-board card never counts: with it the "other" combo would be present.
	addToDeck(t, a, owner, deckID, outsider, "maybe")

	// No chosen bracket: an estimate and the cards, but no findings.
	rec := serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/bracket", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get bracket: %d %s", rec.Code, rec.Body.String())
	}
	report := decode[deckBracketJSON](t, rec)
	if !report.ComboDataAvailable {
		t.Error("combo_data_available = false, want true")
	}
	if report.EstimatedBracket != 4 || report.Tag != bracket.Ruthless {
		t.Errorf("estimate = %d (%s), want 4 (Ruthless)", report.EstimatedBracket, report.Tag)
	}
	if report.GameChangers.Count != 1 || len(report.GameChangers.Cards) != 1 {
		t.Errorf("game changers = %+v, want the one tutor", report.GameChangers)
	}
	if len(report.Combos) != 1 || report.Combos[0].ID != comboID {
		t.Errorf("combos = %+v, want only %s (the other needs a maybe-board card)", report.Combos, comboID)
	}
	if len(report.MassLandDenial) != 1 || len(report.ExtraTurns) != 1 {
		t.Errorf("mld/extra turns = %v / %v, want one each", report.MassLandDenial, report.ExtraTurns)
	}
	if report.TargetBracket != nil || len(report.Findings) != 0 {
		t.Errorf("target/findings = %v / %v, want none while no bracket is chosen", report.TargetBracket, report.Findings)
	}

	// Choose Bracket 2: Game Changer violation, MLD and combo warnings.
	if rec := serveIO(t, a, owner, http.MethodPatch, "/decks/"+deckID, `{"bracket":2}`); rec.Code != http.StatusOK {
		t.Fatalf("set bracket: %d %s", rec.Code, rec.Body.String())
	}
	report = decode[deckBracketJSON](t, serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/bracket", ""))
	if report.TargetBracket == nil || *report.TargetBracket != 2 || report.GameChangers.Limit == nil || *report.GameChangers.Limit != 0 {
		t.Errorf("target/limit = %v / %v, want 2 / 0", report.TargetBracket, report.GameChangers.Limit)
	}
	kinds := map[string]string{}
	for _, f := range report.Findings {
		kinds[f.Kind] = f.Severity
	}
	if kinds[bracket.KindGameChangers] != bracket.SeverityViolation ||
		kinds[bracket.KindMassLandDenial] != bracket.SeverityWarning ||
		kinds[bracket.KindCombo] != bracket.SeverityWarning {
		t.Errorf("findings = %+v, want a game-changer violation plus MLD and combo warnings", report.Findings)
	}

	// Another owner cannot read it (DECK-126).
	if rec := serveIO(t, a, makeUser(t, a), http.MethodGet, "/decks/"+deckID+"/bracket", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner bracket = %d, want 404", rec.Code)
	}
}

// @spec DECK-128
func TestUpdateDeck_BracketValidation(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	deckID := newDeck(t, a, owner)

	patch := func(body string) (int, *int32) {
		rec := serveIO(t, a, owner, http.MethodPatch, "/decks/"+deckID, body)
		if rec.Code != http.StatusOK {
			return rec.Code, nil
		}
		return rec.Code, decode[deckJSON](t, rec).Bracket
	}

	if code, b := patch(`{"bracket":3}`); code != http.StatusOK || b == nil || *b != 3 {
		t.Fatalf("set 3 = %d %v", code, b)
	}
	if code, b := patch(`{"name":"renamed"}`); code != http.StatusOK || b == nil || *b != 3 {
		t.Fatalf("absent key must leave the bracket: %d %v", code, b)
	}
	for _, bad := range []string{`{"bracket":0}`, `{"bracket":6}`, `{"bracket":"3"}`, `{"bracket":2.5}`, `{"bracket":true}`} {
		if code, _ := patch(bad); code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", bad, code)
		}
	}
	if code, b := patch(`{"name":"x"}`); code != http.StatusOK || b == nil || *b != 3 {
		t.Fatalf("a rejected update must not change the deck: %d %v", code, b)
	}
	if code, b := patch(`{"bracket":null}`); code != http.StatusOK || b != nil {
		t.Fatalf("null must clear the bracket: %d %v", code, b)
	}
}
