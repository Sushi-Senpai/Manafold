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
