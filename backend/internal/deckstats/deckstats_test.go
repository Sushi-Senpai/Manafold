package deckstats

import "testing"

// @spec DECK-050
func TestAnalyze_TypeCountsAndAvgManaValue(t *testing.T) {
	cards := []CardStat{
		{TypeLine: "Legendary Creature — Goblin", ManaValue: 3, Quantity: 1},
		{TypeLine: "Creature — Goblin", ManaValue: 1, Quantity: 4},
		{TypeLine: "Artifact", ManaValue: 1, Quantity: 1},
		{TypeLine: "Basic Land — Mountain", ManaValue: 0, Quantity: 10, IsLand: true},
		{TypeLine: "Sorcery", ManaValue: 4, Quantity: 1},
	}
	s := Analyze(cards)

	if s.TypeCounts["Creature"] != 5 {
		t.Fatalf("Creature count = %d, want 5", s.TypeCounts["Creature"])
	}
	if s.TypeCounts["Land"] != 10 {
		t.Fatalf("Land count = %d, want 10", s.TypeCounts["Land"])
	}
	if s.TypeCounts["Artifact"] != 1 || s.TypeCounts["Sorcery"] != 1 {
		t.Fatalf("unexpected type counts: %#v", s.TypeCounts)
	}

	// Non-land mana values: 3 + (1*4) + 1 + 4 = 12 over 7 non-land cards.
	want := 12.0 / 7.0
	if diff := s.AvgManaValue - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("AvgManaValue = %v, want %v", s.AvgManaValue, want)
	}
}

func TestAnalyze_EmptyDeck(t *testing.T) {
	s := Analyze(nil)
	if s.AvgManaValue != 0 || len(s.TypeCounts) != 0 {
		t.Fatalf("empty deck stats not zero-valued: %#v", s)
	}
}

// @spec DECK-051
func TestAnalyze_ManaCurvePipsAndSources(t *testing.T) {
	cards := []CardStat{
		{TypeLine: "Legendary Creature — Angel", ManaCost: "{3}{W}{W}", ManaValue: 5, Quantity: 1},
		{TypeLine: "Instant", ManaCost: "{W}{U}", ManaValue: 2, Quantity: 1},
		{TypeLine: "Sorcery", ManaCost: "{8}", ManaValue: 8, Quantity: 1},
		{TypeLine: "Artifact", ManaCost: "{2}", ManaValue: 2, Quantity: 1, ProducedMana: []string{"W", "U", "B", "R", "G"}},
		{TypeLine: "Basic Land — Plains", ManaValue: 0, Quantity: 10, IsLand: true, ProducedMana: []string{"W"}},
	}
	s := Analyze(cards)

	if s.ManaCurve["2"] != 2 {
		t.Errorf("curve[2] = %d, want 2 (the {W}{U} instant and the {2} rock)", s.ManaCurve["2"])
	}
	if s.ManaCurve["5"] != 1 {
		t.Errorf("curve[5] = %d, want 1", s.ManaCurve["5"])
	}
	if s.ManaCurve["7+"] != 1 {
		t.Errorf("curve[7+] = %d, want 1 (the {8} sorcery)", s.ManaCurve["7+"])
	}
	if _, ok := s.ManaCurve["0"]; ok {
		t.Errorf("lands must not enter the curve: %#v", s.ManaCurve)
	}
	if s.ColorPips["W"] != 3 {
		t.Errorf("W pips = %d, want 3 (two from {W}{W}, one from {W}{U})", s.ColorPips["W"])
	}
	if s.ColorPips["U"] != 1 {
		t.Errorf("U pips = %d, want 1", s.ColorPips["U"])
	}
	if s.ColorSources["W"] != 11 {
		t.Errorf("W sources = %d, want 11 (10 Plains + the 5-colour rock)", s.ColorSources["W"])
	}
	if s.ColorSources["G"] != 1 {
		t.Errorf("G sources = %d, want 1 (the rock only)", s.ColorSources["G"])
	}
	if s.LandCount != 10 || s.NonLandCount != 4 {
		t.Errorf("land/non-land split = %d/%d, want 10/4", s.LandCount, s.NonLandCount)
	}
}

// @spec DECK-051
func TestCountPips_HybridAndPhyrexian(t *testing.T) {
	s := Analyze([]CardStat{
		{TypeLine: "Creature", ManaCost: "{W/U}{W/P}{2/B}", ManaValue: 3, Quantity: 1},
	})
	if s.ColorPips["W"] != 2 {
		t.Errorf("W = %d, want 2 (from {W/U} and {W/P})", s.ColorPips["W"])
	}
	if s.ColorPips["U"] != 1 || s.ColorPips["B"] != 1 {
		t.Errorf("U/B = %d/%d, want 1/1", s.ColorPips["U"], s.ColorPips["B"])
	}
}

// @spec DECK-052
func TestAnalyze_CategoryVocabularyRollup(t *testing.T) {
	cards := []CardStat{
		{TypeLine: "Artifact", ManaValue: 2, Quantity: 1, Category: "ramp"},
		{TypeLine: "Creature", ManaValue: 1, Quantity: 1, Category: "Mana Dork"},
		{TypeLine: "Sorcery", ManaValue: 4, Quantity: 1, Category: "Wrath"},
		{TypeLine: "Instant", ManaValue: 2, Quantity: 1, Category: "Spot Removal"},
		{TypeLine: "Enchantment", ManaValue: 3, Quantity: 1, Category: "Group Hug"},
		{TypeLine: "Creature", ManaValue: 3, Quantity: 1},
	}
	s := Analyze(cards)

	if s.CategoryCounts["Ramp"] != 2 {
		t.Errorf("Ramp = %d, want 2 (\"ramp\" + \"Mana Dork\" synonym)", s.CategoryCounts["Ramp"])
	}
	if s.CategoryCounts["Board Wipe"] != 1 {
		t.Errorf("Board Wipe = %d, want 1 (\"Wrath\")", s.CategoryCounts["Board Wipe"])
	}
	if s.CategoryCounts["Removal"] != 1 {
		t.Errorf("Removal = %d, want 1", s.CategoryCounts["Removal"])
	}
	if s.CategoryCounts["Group Hug"] != 1 {
		t.Errorf("free-text category should pass through verbatim: %#v", s.CategoryCounts)
	}
	if _, ok := s.CategoryCounts[""]; ok {
		t.Errorf("uncategorised cards must not create an empty bucket: %#v", s.CategoryCounts)
	}
}

func TestSortedCurve_Order(t *testing.T) {
	got := SortedCurve(map[string]int{"7+": 1, "2": 3, "0": 1, "5": 2})
	want := []string{"0", "2", "5", "7+"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedCurve = %v, want %v", got, want)
		}
	}
}
