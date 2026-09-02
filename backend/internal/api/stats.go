package api

import (
	"net/http"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckstats"
)

// deckStatsJSON is the stats payload: the deterministic Analyze result plus the
// Commander rules-of-thumb targets the category roll-up is meant to be read
// against (DECK-051).
type deckStatsJSON struct {
	deckstats.Stats
	CategoryTargets map[string][2]int `json:"category_targets"`
}

// @spec DECK-051, DECK-052
func (a *API) getDeckStats(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}

	var stats []deckstats.CardStat
	if ld.commander != nil {
		stats = append(stats, cardStatFromCard(*ld.commander, 1, ""))
	}
	if ld.partner != nil {
		stats = append(stats, cardStatFromCard(*ld.partner, 1, ""))
	}
	for _, e := range ld.entries {
		// The mana curve, pip demand, and category roll-up describe the deck the
		// player is building — the main and command boards — not the maybe /
		// sideboard staging areas, matching what /validation counts.
		if e.Board != "main" && e.Board != "command" {
			continue
		}
		stats = append(stats, deckstats.CardStat{
			TypeLine:     e.TypeLine,
			ManaCost:     e.ManaCost.String,
			ManaValue:    e.ManaValue,
			Quantity:     int(e.Quantity),
			IsLand:       isBasicLand(e.TypeLine),
			ProducedMana: nonNil(e.ProducedMana),
			Category:     e.Category.String,
		})
	}

	writeJSON(w, http.StatusOK, deckStatsJSON{
		Stats:           deckstats.Analyze(stats),
		CategoryTargets: deckstats.CategoryTargets,
	})
}

func cardStatFromCard(c db.Card, qty int, category string) deckstats.CardStat {
	return deckstats.CardStat{
		TypeLine:     c.TypeLine,
		ManaCost:     c.ManaCost.String,
		ManaValue:    c.ManaValue,
		Quantity:     qty,
		IsLand:       isBasicLand(c.TypeLine),
		ProducedMana: nonNil(c.ProducedMana),
		Category:     category,
	}
}
