// Package decktemplates is the one authoritative definition of Manafold's deck
// templates: per-archetype count ranges, out of the 99 non-commander cards, for
// the card types (lands, creatures, instants & sorceries, artifacts,
// enchantments) and the functional roles (ramp, card draw, targeted removal,
// board wipes). The builder's "deck vs. target" panel and the later
// deck-building wizard both read this data — served verbatim at
// GET /api/deck-templates — so no client carries its own copy of a range.
// Templates are guidance, never validation. Pure: no DB, no AI.
//
// @spec DECK-100, DECK-101, DECK-102, DECK-104, DECK-105, DECK-108
package decktemplates

import "manafold-backend/internal/deckroles"

// Version is bumped whenever any template or range changes, so a consumer that
// cached the templates (or a saved wizard state) can tell its copy is stale.
const Version = 1

// Kind says whether a row counts a card type or a functional role.
type Kind string

const (
	KindType Kind = "type"
	KindRole Kind = "role"
)

// Row keys. The role keys are the deckroles.Role values, so role counts map
// onto rows directly.
const (
	RowLands             = "lands"
	RowCreatures         = "creatures"
	RowInstantsSorceries = "instants_sorceries"
	RowArtifacts         = "artifacts"
	RowEnchantments      = "enchantments"
	RowRamp              = string(deckroles.Ramp)
	RowCardDraw          = string(deckroles.CardDraw)
	RowTargetedRemoval   = string(deckroles.TargetedRemoval)
	RowBoardWipes        = string(deckroles.BoardWipe)
)

// A Bracket 4 deck (heavy fast mana) may run fewer lands than its template's
// land minimum, down to LandRelaxedFloor. Only Bracket 4 relaxes: Bracket 5 and
// below 4 read against the normal range. Whether the deck actually runs fast
// mana is not checked yet (deferred until role tags can count it).
const (
	LandRelaxedAtBracket = 4
	LandRelaxedFloor     = 32
)

// Range is an inclusive count range.
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Row is one target of a template. RelaxedAtBracket, when non-zero, is the one
// deck bracket at which a count below Min but at or above RelaxedFloor is
// acceptable.
type Row struct {
	Key              string `json:"key"`
	Label            string `json:"label"`
	Kind             Kind   `json:"kind"`
	Min              int    `json:"min"`
	Max              int    `json:"max"`
	RelaxedAtBracket int    `json:"relaxed_at_bracket,omitempty"`
	RelaxedFloor     int    `json:"relaxed_floor,omitempty"`
}

// Template is one archetype's set of targets, rows in display order.
type Template struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Rows []Row  `json:"rows"`
}

// Role targets shared by every archetype (Control overrides board wipes).
var (
	rampTarget    = Range{10, 12}
	drawTarget    = Range{10, 12}
	removalTarget = Range{8, 10}
	wipeTarget    = Range{2, 4}
)

// templates is the captain-approved table (MANAFOLD.md § Deck templates).
var templates = []Template{
	build("go_wide_tribal", "Go-wide / Tribal", Range{36, 38}, Range{28, 35}, Range{10, 14}, Range{8, 11}, Range{4, 8}, wipeTarget),
	build("voltron", "Voltron", Range{35, 38}, Range{18, 24}, Range{10, 14}, Range{14, 20}, Range{6, 12}, wipeTarget),
	build("spellslinger", "Spellslinger", Range{34, 38}, Range{10, 16}, Range{28, 36}, Range{8, 12}, Range{2, 6}, wipeTarget),
	build("midrange_value", "Midrange / Value", Range{36, 38}, Range{25, 32}, Range{14, 20}, Range{8, 12}, Range{4, 8}, wipeTarget),
	build("control", "Control", Range{36, 38}, Range{12, 18}, Range{25, 32}, Range{8, 12}, Range{4, 8}, Range{4, 6}),
	build("ramp_big_creatures", "Ramp / Big creatures", Range{38, 40}, Range{22, 28}, Range{12, 18}, Range{10, 14}, Range{3, 7}, wipeTarget),
	build("artifacts", "Artifacts", Range{34, 38}, Range{12, 20}, Range{8, 14}, Range{28, 36}, Range{0, 4}, wipeTarget),
	build("enchantress", "Enchantress", Range{36, 38}, Range{12, 18}, Range{10, 14}, Range{6, 10}, Range{24, 32}, wipeTarget),
	build("aristocrats", "Aristocrats (sacrifice)", Range{36, 38}, Range{30, 38}, Range{10, 14}, Range{8, 12}, Range{4, 8}, wipeTarget),
	build("lands_matter", "Lands-matter", Range{40, 44}, Range{18, 24}, Range{14, 18}, Range{6, 10}, Range{4, 8}, wipeTarget),
}

func build(key, name string, lands, creatures, spells, artifacts, enchantments, wipes Range) Template {
	return Template{Key: key, Name: name, Rows: []Row{
		{Key: RowLands, Label: "Lands", Kind: KindType, Min: lands.Min, Max: lands.Max, RelaxedAtBracket: LandRelaxedAtBracket, RelaxedFloor: LandRelaxedFloor},
		{Key: RowCreatures, Label: "Creatures", Kind: KindType, Min: creatures.Min, Max: creatures.Max},
		{Key: RowInstantsSorceries, Label: "Instants & sorceries", Kind: KindType, Min: spells.Min, Max: spells.Max},
		{Key: RowArtifacts, Label: "Artifacts", Kind: KindType, Min: artifacts.Min, Max: artifacts.Max},
		{Key: RowEnchantments, Label: "Enchantments", Kind: KindType, Min: enchantments.Min, Max: enchantments.Max},
		{Key: RowRamp, Label: "Ramp", Kind: KindRole, Min: rampTarget.Min, Max: rampTarget.Max},
		{Key: RowCardDraw, Label: "Card draw", Kind: KindRole, Min: drawTarget.Min, Max: drawTarget.Max},
		{Key: RowTargetedRemoval, Label: "Targeted removal", Kind: KindRole, Min: removalTarget.Min, Max: removalTarget.Max},
		{Key: RowBoardWipes, Label: "Board wipes", Kind: KindRole, Min: wipes.Min, Max: wipes.Max},
	}}
}

// All returns every template in display order. The result is a deep copy, so a
// caller cannot mutate the authoritative data.
//
// @spec DECK-100
func All() []Template {
	out := make([]Template, len(templates))
	for i, t := range templates {
		out[i] = clone(t)
	}
	return out
}

// Lookup returns the template with the given key.
func Lookup(key string) (Template, bool) {
	for _, t := range templates {
		if t.Key == key {
			return clone(t), true
		}
	}
	return Template{}, false
}

func clone(t Template) Template {
	t.Rows = append([]Row(nil), t.Rows...)
	return t
}

// Counts maps the deck's primary-type counts (deckstats.Stats.TypeCounts, keyed
// "Land", "Creature", …) and role counts onto template row keys. Instants and
// sorceries share one row; planeswalkers, battles, and other types fill none.
//
// @spec DECK-105
func Counts(typeCounts map[string]int, roleCounts map[deckroles.Role]int) map[string]int {
	out := map[string]int{
		RowLands:             typeCounts["Land"],
		RowCreatures:         typeCounts["Creature"],
		RowInstantsSorceries: typeCounts["Instant"] + typeCounts["Sorcery"],
		RowArtifacts:         typeCounts["Artifact"],
		RowEnchantments:      typeCounts["Enchantment"],
	}
	for role, n := range roleCounts {
		out[string(role)] = n
	}
	return out
}

// Status is one row's reading against its range.
type Status string

const (
	StatusOK    Status = "ok"
	StatusUnder Status = "under"
	StatusOver  Status = "over"
	// StatusRelaxed is a count below Min, at or above RelaxedFloor, that the
	// deck's bracket permits.
	StatusRelaxed Status = "relaxed"
)

// RowFit is one template row with the deck's count and status.
type RowFit struct {
	Row
	Count  int    `json:"count"`
	Status Status `json:"status"`
}

// Evaluate reads counts (keyed by row key; a missing key counts as 0) against
// every row of t. bracket is the deck's self-assigned bracket, 0 when unset.
//
// @spec DECK-104, DECK-108
func Evaluate(t Template, counts map[string]int, bracket int) []RowFit {
	out := make([]RowFit, 0, len(t.Rows))
	for _, r := range t.Rows {
		n := counts[r.Key]
		status := StatusOK
		switch {
		case n > r.Max:
			status = StatusOver
		case n < r.Min && r.RelaxedAtBracket > 0 && bracket == r.RelaxedAtBracket && n >= r.RelaxedFloor:
			status = StatusRelaxed
		case n < r.Min:
			status = StatusUnder
		}
		out = append(out, RowFit{Row: r, Count: n, Status: status})
	}
	return out
}
