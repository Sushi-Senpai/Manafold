// Package deckstats is the pure deck analyser. M1 ships only type counts and the
// average mana value; the mana curve, colour-pip-vs-source counts, and the
// functional-category rollup against Commander rules-of-thumb land at M2/M5
// (DECK-051, DECK-052). Kept pure so it is unit-testable from day one.
//
// @spec DECK-050
package deckstats

import "strings"

// CardStat is the minimal per-card input: one entry per physical copy is
// expanded by the caller, or Quantity carries the count.
type CardStat struct {
	TypeLine  string
	ManaValue float64
	Quantity  int
	IsLand    bool
}

// Stats is the analysis result. Only TypeCounts and AvgManaValue are populated
// in M1.
type Stats struct {
	TypeCounts   map[string]int `json:"type_counts"`
	AvgManaValue float64        `json:"avg_mana_value"`
	// Reserved for M2/M5.
	ManaCurve      map[int]int    `json:"mana_curve,omitempty"`
	CategoryCounts map[string]int `json:"category_counts,omitempty"`
}

// primaryTypes are the card supertypes/types deckstats buckets by, checked in
// order so a "Legendary Creature — God" lands under "Creature".
var primaryTypes = []string{
	"Creature", "Planeswalker", "Land", "Artifact", "Enchantment",
	"Instant", "Sorcery", "Battle",
}

// Analyze returns type counts and the average mana value of the non-land cards.
func Analyze(cards []CardStat) Stats {
	s := Stats{TypeCounts: map[string]int{}}

	var mvSum float64
	var mvCount int
	for _, c := range cards {
		qty := c.Quantity
		if qty <= 0 {
			qty = 1
		}
		s.TypeCounts[primaryType(c.TypeLine)] += qty
		if !c.IsLand && !strings.Contains(c.TypeLine, "Land") {
			mvSum += c.ManaValue * float64(qty)
			mvCount += qty
		}
	}
	if mvCount > 0 {
		s.AvgManaValue = mvSum / float64(mvCount)
	}
	return s
}

func primaryType(typeLine string) string {
	for _, t := range primaryTypes {
		if strings.Contains(typeLine, t) {
			return t
		}
	}
	return "Other"
}
