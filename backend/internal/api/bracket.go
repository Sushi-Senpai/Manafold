package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/bracket"
	db "manafold-backend/internal/db/generated"
)

// deckBracketJSON is the bracket report plus whether the Commander Spellbook
// combo mirror has been loaded, so the builder can say "combo data not loaded"
// instead of implying the deck has no combos (DECK-126).
type deckBracketJSON struct {
	bracket.Report
	ComboDataAvailable bool `json:"combo_data_available"`
}

// getDeckBracket returns the deterministic bracket report for a deck: its
// estimated bracket, Game Changer count against the chosen bracket, present
// Commander Spellbook combos, and mass-land-denial / extra-turn cards. It reads
// only the local mirror; no Commander Spellbook request is made (CARD-056).
//
// @spec DECK-120, DECK-121, DECK-122, DECK-124, DECK-125, DECK-126
func (a *API) getDeckBracket(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	ctx := r.Context()

	cards, oracleIDs, commanders := bracketDeckCards(ld)
	flagged, err := a.spellbookFlags(ctx, oracleIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load bracket data")
		return
	}
	for i := range cards {
		applyFlags(&cards[i], flagged)
	}

	combos, err := a.loadCombos(r, oracleIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load bracket data")
		return
	}
	available, err := a.Queries.ComboDataAvailable(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load bracket data")
		return
	}

	in := bracket.Input{Cards: cards, Commanders: commanders, Combos: combos}
	if deck.Bracket.Valid {
		b := int(deck.Bracket.Int32)
		in.TargetBracket = &b
	}
	writeJSON(w, http.StatusOK, deckBracketJSON{Report: bracket.Check(in), ComboDataAvailable: available})
}

// bracketDeckCards lists the cards the bracket check reads — the commander(s)
// and the main and command entries, like the stats and validation views — with
// their oracle ids and the oracle ids of the commanders.
func bracketDeckCards(ld *loadedDeck) (cards []bracket.Card, oracleIDs []pgtype.UUID, commanders []string) {
	add := func(oracle pgtype.UUID, name string, qty int, gameChanger bool) {
		cards = append(cards, bracket.Card{
			OracleID: uuidString(oracle), Name: name, Quantity: qty, GameChanger: gameChanger,
		})
		oracleIDs = append(oracleIDs, oracle)
	}
	for _, c := range []*db.Card{ld.commander, ld.partner} {
		if c != nil {
			add(c.ScryfallOracleID, c.Name, 1, c.IsGameChanger)
			commanders = append(commanders, uuidString(c.ScryfallOracleID))
		}
	}
	for _, e := range ld.entries {
		if e.Board != "main" && e.Board != "command" {
			continue
		}
		add(e.ScryfallOracleID, e.Name, int(e.Quantity), e.IsGameChanger)
		if e.Board == "command" {
			commanders = append(commanders, uuidString(e.ScryfallOracleID))
		}
	}
	return cards, oracleIDs, commanders
}

// spellbookFlags reads Commander Spellbook's mass-land-denial / extra-turn flags
// for the given oracle ids, keyed by oracle id string.
func (a *API) spellbookFlags(ctx context.Context, oracleIDs []pgtype.UUID) (map[string]db.SpellbookCardFlag, error) {
	flags, err := a.Queries.ListSpellbookCardFlags(ctx, oracleIDs)
	if err != nil {
		return nil, err
	}
	flagged := make(map[string]db.SpellbookCardFlag, len(flags))
	for _, f := range flags {
		flagged[uuidString(f.ScryfallOracleID)] = f
	}
	return flagged, nil
}

// applyFlags copies a card's Spellbook flags onto its bracket.Card.
func applyFlags(c *bracket.Card, flagged map[string]db.SpellbookCardFlag) {
	if f, ok := flagged[c.OracleID]; ok {
		c.MassLandDenial = f.MassLandDenial
		c.ExtraTurn = f.ExtraTurn
	}
}

// loadCombos reads the mirrored combos whose card ingredients all lie within
// the deck's cards, with their ingredients, as bracket.Combo values (DECK-122).
func (a *API) loadCombos(r *http.Request, oracleIDs []pgtype.UUID) ([]bracket.Combo, error) {
	ctx := r.Context()
	rows, err := a.Queries.ListCombosWithin(ctx, oracleIDs)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	ingredientRows, err := a.Queries.ListComboCards(ctx, ids)
	if err != nil {
		return nil, err
	}
	byCombo := make(map[string][]bracket.Ingredient, len(rows))
	for _, ic := range ingredientRows {
		byCombo[ic.ComboID] = append(byCombo[ic.ComboID], bracket.Ingredient{
			OracleID:        uuidString(ic.ScryfallOracleID),
			Quantity:        int(ic.Quantity),
			ZoneLocations:   ic.ZoneLocations,
			MustBeCommander: ic.MustBeCommander,
		})
	}
	combos := make([]bracket.Combo, 0, len(rows))
	for _, row := range rows {
		var produces []bracket.Feature
		if err := json.Unmarshal(row.Produces, &produces); err != nil {
			return nil, err
		}
		combos = append(combos, bracket.Combo{
			ID:                      row.ID,
			ManaNeeded:              row.ManaNeeded,
			ManaValueNeeded:         int(row.ManaValueNeeded),
			HasNotablePrerequisites: row.HasNotablePrerequisites,
			Ingredients:             byCombo[row.ID],
			Produces:                produces,
		})
	}
	return combos, nil
}
