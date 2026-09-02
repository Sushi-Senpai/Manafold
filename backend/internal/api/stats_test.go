package api

import (
	"encoding/hex"
	"net/http"
	"testing"

	"manafold-backend/internal/deckstats"
)

// @spec DECK-051, DECK-052
func TestDeckStats_EndToEnd(t *testing.T) {
	a := testAPI(t)
	owner := makeUser(t, a)
	sfx := hex.EncodeToString(randBytes(t, 4))

	cmd := mkCard(t, a, "Stats Commander "+sfx, "Legendary Creature — Elf", "{1}{G}", 2, []string{"G"}, nil, true)
	dork := mkCard(t, a, "Stats Dork "+sfx, "Creature — Elf Druid", "{G}", 1, []string{"G"}, []string{"G"}, false)
	bomb := mkCard(t, a, "Stats Bomb "+sfx, "Creature — Wurm", "{6}{G}{G}", 8, []string{"G"}, nil, false)
	forest := mkCard(t, a, "Stats Forest "+sfx, "Basic Land — Forest", "", 0, []string{"G"}, []string{"G"}, false)

	deckID := newDeck(t, a, owner)
	if rec := serveIO(t, a, owner, http.MethodPut, "/decks/"+deckID+"/commander", `{"commander_card_id":"`+uuidString(cmd)+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct {
		id       string
		category string
	}{
		{uuidString(dork), "Ramp"},
		{uuidString(bomb), "Threat"},
		{uuidString(forest), ""},
	} {
		body := `{"card_id":"` + c.id + `","board":"main"`
		if c.category != "" {
			body += `,"category":"` + c.category + `"`
		}
		body += `}`
		if rec := serveIO(t, a, owner, http.MethodPost, "/decks/"+deckID+"/cards", body); rec.Code != http.StatusCreated {
			t.Fatalf("add card: %d %s", rec.Code, rec.Body.String())
		}
	}

	rec := serveIO(t, a, owner, http.MethodGet, "/decks/"+deckID+"/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get stats: %d %s", rec.Code, rec.Body.String())
	}
	stats := decode[deckStatsJSON](t, rec)

	if stats.LandCount != 1 {
		t.Errorf("land_count = %d, want 1", stats.LandCount)
	}
	// non-land: commander {1}{G} (2), dork {G} (1), bomb {6}{G}{G} (8 → 7+)
	if stats.ManaCurve["1"] != 1 || stats.ManaCurve["2"] != 1 || stats.ManaCurve["7+"] != 1 {
		t.Errorf("mana_curve = %#v, want 1@1, 1@2, 1@7+", stats.ManaCurve)
	}
	if stats.ColorPips["G"] != 4 {
		t.Errorf("G pips = %d, want 4 (1 + 1 + 2)", stats.ColorPips["G"])
	}
	if stats.ColorSources["G"] != 2 {
		t.Errorf("G sources = %d, want 2 (dork + forest)", stats.ColorSources["G"])
	}
	if stats.CategoryCounts["Ramp"] != 1 || stats.CategoryCounts["Threat"] != 1 {
		t.Errorf("category_counts = %#v, want Ramp:1 Threat:1", stats.CategoryCounts)
	}
	if got := stats.CategoryTargets["Land"]; got != deckstats.CategoryTargets["Land"] {
		t.Errorf("category_targets not echoed: %#v", stats.CategoryTargets)
	}

	// non-owner 404 (DECK-009)
	stranger := makeUser(t, a)
	if rec := serveIO(t, a, stranger, http.MethodGet, "/decks/"+deckID+"/stats", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner stats = %d, want 404", rec.Code)
	}
}
