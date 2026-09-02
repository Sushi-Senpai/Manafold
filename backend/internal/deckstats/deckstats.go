// Package deckstats is the pure, deterministic deck analyser: type counts, the
// mana curve, colour-pip demand against colour sources, average mana value, and
// a roll-up of functional categories against Commander rules-of-thumb. No DB, no
// AI — the handler assembles the input from already-loaded rows and calls
// Analyze, so the whole thing is exhaustively table-testable. An LLM prose
// summary over these numbers is a later milestone (M5); the numbers themselves
// are deterministic and ship now.
//
// @spec DECK-050, DECK-051, DECK-052
package deckstats

import (
	"sort"
	"strconv"
	"strings"
)

// CardStat is the per-entry input. Quantity carries the copy count; the caller
// does not pre-expand.
type CardStat struct {
	TypeLine     string
	ManaCost     string // Scryfall mana-cost string, e.g. "{2}{W}{W}"
	ManaValue    float64
	Quantity     int
	IsLand       bool
	ProducedMana []string // colours this card can add, from Scryfall produced_mana
	Category     string   // deck_cards.category (free text; DECK-052 rolls the known ones up)
}

// Stats is the analysis result.
type Stats struct {
	TypeCounts     map[string]int `json:"type_counts"`
	AvgManaValue   float64        `json:"avg_mana_value"`
	ManaCurve      map[string]int `json:"mana_curve"`      // "0".."6","7+" → non-land card count
	ColorPips      map[string]int `json:"color_pips"`      // W/U/B/R/G → coloured pips demanded by mana costs
	ColorSources   map[string]int `json:"color_sources"`   // W/U/B/R/G → permanents that can produce that colour
	CategoryCounts map[string]int `json:"category_counts"` // canonicalised known categories + verbatim free-text
	LandCount      int            `json:"land_count"`
	NonLandCount   int            `json:"nonland_count"`
}

// primaryTypes are the card types deckstats buckets by, checked in order so a
// "Legendary Creature — God" lands under "Creature".
var primaryTypes = []string{
	"Creature", "Planeswalker", "Land", "Artifact", "Enchantment",
	"Instant", "Sorcery", "Battle",
}

// KnownCategories is the closed vocabulary DECK-052 rolls counts up by. A
// deck_cards.category that case-insensitively matches one of these (or a listed
// synonym) is counted under the canonical name; anything else is kept verbatim
// as a free-text escape hatch.
var KnownCategories = []string{
	"Ramp", "Card Draw", "Removal", "Board Wipe", "Counterspell",
	"Land", "Protection", "Recursion", "Tutor", "Threat",
}

var categorySynonyms = map[string]string{
	"ramp":           "Ramp",
	"mana ramp":      "Ramp",
	"mana rock":      "Ramp",
	"mana rocks":     "Ramp",
	"dork":           "Ramp",
	"mana dork":      "Ramp",
	"mana dorks":     "Ramp",
	"card draw":      "Card Draw",
	"draw":           "Card Draw",
	"card advantage": "Card Draw",
	"removal":        "Removal",
	"spot removal":   "Removal",
	"interaction":    "Removal",
	"board wipe":     "Board Wipe",
	"boardwipe":      "Board Wipe",
	"wrath":          "Board Wipe",
	"sweeper":        "Board Wipe",
	"mass removal":   "Board Wipe",
	"counterspell":   "Counterspell",
	"counter":        "Counterspell",
	"counters":       "Counterspell",
	"land":           "Land",
	"lands":          "Land",
	"protection":     "Protection",
	"recursion":      "Recursion",
	"reanimation":    "Recursion",
	"tutor":          "Tutor",
	"tutors":         "Tutor",
	"threat":         "Threat",
	"threats":        "Threat",
	"finisher":       "Threat",
	"win condition":  "Threat",
	"wincon":         "Threat",
}

// CategoryTargets are the Commander deckbuilding rules-of-thumb (a common
// starting-point spread for a ~99-card singleton deck) the category roll-up is
// meant to be read against. They are guidance the UI and ai-assist compare to,
// not validation — a deck outside a band is not "illegal".
var CategoryTargets = map[string][2]int{
	"Land":         {36, 38},
	"Ramp":         {8, 12},
	"Card Draw":    {8, 12},
	"Removal":      {8, 12},
	"Board Wipe":   {3, 5},
	"Counterspell": {0, 8},
}

// Analyze returns the deterministic per-deck statistics.
//
// @spec DECK-050, DECK-051, DECK-052
func Analyze(cards []CardStat) Stats {
	s := Stats{
		TypeCounts:     map[string]int{},
		ManaCurve:      map[string]int{},
		ColorPips:      map[string]int{},
		ColorSources:   map[string]int{},
		CategoryCounts: map[string]int{},
	}

	var mvSum float64
	var mvCount int
	for _, c := range cards {
		qty := c.Quantity
		if qty <= 0 {
			qty = 1
		}
		isLand := c.IsLand || strings.Contains(c.TypeLine, "Land")

		s.TypeCounts[primaryType(c.TypeLine)] += qty

		if isLand {
			s.LandCount += qty
		} else {
			s.NonLandCount += qty
			s.ManaCurve[curveBucket(c.ManaValue)] += qty
			mvSum += c.ManaValue * float64(qty)
			mvCount += qty
			for color, n := range countPips(c.ManaCost) {
				s.ColorPips[color] += n * qty
			}
		}

		for _, color := range c.ProducedMana {
			color = strings.ToUpper(strings.TrimSpace(color))
			if isWUBRG(color) {
				s.ColorSources[color] += qty
			}
		}

		if cat := canonicalCategory(c.Category); cat != "" {
			s.CategoryCounts[cat] += qty
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

// curveBucket maps a mana value to a curve column: 0..6 by rounded-down integer,
// everything 7 and above collapsed into "7+".
func curveBucket(mv float64) string {
	n := int(mv)
	if n < 0 {
		n = 0
	}
	if n >= 7 {
		return "7+"
	}
	return strconv.Itoa(n)
}

// countPips counts coloured mana symbols in a Scryfall mana-cost string. Each
// {...} symbol contributes 1 to every WUBRG letter it names, so a hybrid
// "{W/U}" counts for both W and U, and Phyrexian "{W/P}" counts for W. Generic
// and colourless symbols contribute nothing.
func countPips(manaCost string) map[string]int {
	out := map[string]int{}
	sym := ""
	inSym := false
	for _, r := range manaCost {
		switch r {
		case '{':
			inSym, sym = true, ""
		case '}':
			seen := map[string]bool{}
			for _, ch := range sym {
				c := strings.ToUpper(string(ch))
				if isWUBRG(c) && !seen[c] {
					out[c]++
					seen[c] = true
				}
			}
			inSym = false
		default:
			if inSym {
				sym += string(r)
			}
		}
	}
	return out
}

func isWUBRG(s string) bool {
	switch s {
	case "W", "U", "B", "R", "G":
		return true
	}
	return false
}

// canonicalCategory folds a free-text category to its canonical known name when
// it matches the vocabulary or a synonym (DECK-052), otherwise returns the
// trimmed original as a free-text escape hatch. Empty in, empty out.
func canonicalCategory(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	if canon, ok := categorySynonyms[strings.ToLower(t)]; ok {
		return canon
	}
	for _, known := range KnownCategories {
		if strings.EqualFold(known, t) {
			return known
		}
	}
	return t
}

// SortedCurve returns the curve buckets in display order ("0".."6","7+"),
// including zero-count buckets up to the deck's highest, so a caller can render
// a gap-free axis.
func SortedCurve(curve map[string]int) []string {
	keys := make([]string, 0, len(curve))
	for k := range curve {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return curveRank(keys[i]) < curveRank(keys[j]) })
	return keys
}

func curveRank(bucket string) int {
	if bucket == "7+" {
		return 7
	}
	n := 0
	for _, ch := range bucket {
		n = n*10 + int(ch-'0')
	}
	return n
}
