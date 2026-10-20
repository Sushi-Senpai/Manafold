// Package bracket estimates which Commander Bracket a deck plays at and checks
// it against the bracket the owner chose. It is a pure function of plain
// structs — no database, no AI — exactly like deckrules and deckstats; the
// handler assembles the input from the deck plus card-data's Commander
// Spellbook mirror.
//
// The estimate is a Go port of Commander Spellbook's estimate_bracket
// classifier (backend/spellbook/models/variant.py in
// github.com/SpaceCowMedia/commander-spellbook-backend, MIT License, Copyright
// (c) 2023 Commander-Spellbook). It differs from Spellbook's live endpoint in
// two deliberate ways: every combo's mana figure is treated as an accurate
// minimum (the export does not carry Spellbook's accuracy flag), and banned
// cards do not change the estimate (legality is deckrules' report).
//
// Only the Game Changer count is a hard rule; combos, mass land denial and
// extra turns are reported as warnings and notices. Nothing here rejects a
// deck mutation.
//
// @spec DECK-120, DECK-121, DECK-122, DECK-123, DECK-124, DECK-125
package bracket

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Tag is one of Commander Spellbook's bracket tags.
type Tag string

const (
	Exhibition Tag = "E"
	Core       Tag = "C"
	Oddball    Tag = "O"
	Powerful   Tag = "P"
	Spicy      Tag = "S"
	Ruthless   Tag = "R"
)

// Label is the tag's Spellbook name.
func (t Tag) Label() string {
	switch t {
	case Exhibition:
		return "Exhibition"
	case Core:
		return "Core"
	case Oddball:
		return "Oddball"
	case Powerful:
		return "Powerful"
	case Spicy:
		return "Spicy"
	case Ruthless:
		return "Ruthless"
	}
	return string(t)
}

// MinBracket is the lowest bracket the tag fits, and whether the tag is
// borderline (Oddball may read as 3, Spicy as 4).
func (t Tag) MinBracket() (bracket int, borderline bool) {
	switch t {
	case Core:
		return 2, false
	case Oddball:
		return 2, true
	case Powerful:
		return 3, false
	case Spicy:
		return 3, true
	case Ruthless:
		return 4, false
	}
	return 1, false
}

// Spellbook feature statuses: standalone and contextual features make a combo
// relevant; helper and utility features do not.
const (
	FeatureStandalone = "S"
	FeatureContextual = "C"
)

// Zone letter for an ingredient that starts in the library (not counted).
const zoneLibrary = "L"

// Card is one main- or command-board card of the deck.
type Card struct {
	OracleID       string
	Name           string
	Quantity       int
	GameChanger    bool
	MassLandDenial bool
	ExtraTurn      bool
}

// Ingredient is one card a combo needs.
type Ingredient struct {
	OracleID        string
	Quantity        int
	ZoneLocations   string
	MustBeCommander bool
}

// Feature is one result a combo produces.
type Feature struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Combo is a Commander Spellbook variant whose card ingredients may all be in
// the deck.
type Combo struct {
	ID                      string
	ManaNeeded              string
	ManaValueNeeded         int
	HasNotablePrerequisites bool
	Ingredients             []Ingredient
	Produces                []Feature
}

// Input is everything Check reads.
type Input struct {
	Cards         []Card   // main + command
	Commanders    []string // oracle ids on the command board
	Combos        []Combo  // candidates; Check re-checks quantities
	TargetBracket *int     // nil = not chosen
}

// ClassifiedCombo is a present combo with its in-deck classification.
type ClassifiedCombo struct {
	ID                   string   `json:"id"`
	Cards                []string `json:"cards"`
	Produces             []string `json:"produces"`
	ManaNeeded           string   `json:"mana_needed"`
	Speed                int      `json:"speed"`
	Relevant             bool     `json:"relevant"`
	BorderlineRelevant   bool     `json:"borderline_relevant"`
	DefinitelyTwoCard    bool     `json:"definitely_two_card"`
	ArguablyTwoCard      bool     `json:"arguably_two_card"`
	MassLandDenial       bool     `json:"mass_land_denial"`
	ExtraTurn            bool     `json:"extra_turn"`
	Lock                 bool     `json:"lock"`
	SkipTurns            bool     `json:"skip_turns"`
	ControlAllOpponents  bool     `json:"control_all_opponents"`
	ControlSomeOpponents bool     `json:"control_some_opponents"`
	Tag                  Tag      `json:"tag"`
	TagLabel             string   `json:"tag_label"`
	MinBracket           int      `json:"min_bracket"`
	Borderline           bool     `json:"borderline"`
}

// Severity of a Finding.
const (
	SeverityViolation = "violation"
	SeverityWarning   = "warning"
	SeverityNotice    = "notice"
)

// Finding kinds.
const (
	KindGameChangers   = "game_changers"
	KindCombo          = "combo"
	KindMassLandDenial = "mass_land_denial"
	KindExtraTurn      = "extra_turn"
)

// Finding is one way the deck sits above (or at the edge of) its chosen
// bracket.
type Finding struct {
	Severity string   `json:"severity"`
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	ComboID  string   `json:"combo_id,omitempty"`
	Cards    []string `json:"cards,omitempty"`
}

// GameChangers is the deck's Game Changer budget.
type GameChangers struct {
	Count int      `json:"count"`
	Limit *int     `json:"limit"` // nil = unlimited, or no target chosen
	Cards []string `json:"cards"`
}

// Report is the result of Check.
type Report struct {
	Tag              Tag               `json:"tag"`
	TagLabel         string            `json:"tag_label"`
	EstimatedBracket int               `json:"estimated_bracket"`
	Borderline       bool              `json:"borderline"`
	TargetBracket    *int              `json:"target_bracket"`
	GameChangers     GameChangers      `json:"game_changers"`
	MassLandDenial   []string          `json:"mass_land_denial"`
	ExtraTurns       []string          `json:"extra_turns"`
	Combos           []ClassifiedCombo `json:"combos"`
	Findings         []Finding         `json:"findings"`
}

// Spellbook's feature-name patterns (variant.py).
var (
	massLandDenialPattern       = regexp.MustCompile(`(?i)mass land (?:destruction|denial|removal)`)
	extraTurnPattern            = regexp.MustCompile(`(?i)(?:near-)?infinite (?:extra )?turns?`)
	extraTurnForOpponentPattern = regexp.MustCompile(`(?i)(?:near-)?infinite (?:extra )?turns? for .* opponent`)
	skipTurnsPattern            = regexp.MustCompile(`(?i)(?:infinite(?:ly)? )?skip (?:(?:all )?(?:your |their )|infinite )?(?:future )?turns?`)
	controlAllOpponentsPattern  = regexp.MustCompile(`(?i)you control (?:your|(up to )?three) opponents`)
	controlSomeOpponentsPattern = regexp.MustCompile(`(?i)you control (?:one|an|(?:up to )?two) opponents`)
)

// GameChangerLimit is the most Game Changers a bracket allows; nil means
// unlimited (Wizards: 0 for Brackets 1–2, 3 for Bracket 3, unlimited for 4–5).
func GameChangerLimit(bracket int) *int {
	switch {
	case bracket <= 2:
		zero := 0
		return &zero
	case bracket == 3:
		three := 3
		return &three
	}
	return nil
}

// Check estimates the deck's bracket and, when a target is chosen, reports
// findings against it.
func Check(in Input) Report {
	names := map[string]string{}
	qty := map[string]int{}
	for _, c := range in.Cards {
		names[c.OracleID] = c.Name
		qty[c.OracleID] += c.Quantity
	}
	commanders := map[string]bool{}
	for _, id := range in.Commanders {
		commanders[id] = true
	}

	var gcCount, extraTurnQty int
	gcNames, mldNames, extraNames := []string{}, []string{}, []string{}
	for _, c := range in.Cards {
		if c.GameChanger {
			gcCount += c.Quantity
			gcNames = append(gcNames, c.Name)
		}
		if c.MassLandDenial {
			mldNames = append(mldNames, c.Name)
		}
		if c.ExtraTurn {
			extraTurnQty += c.Quantity
			extraNames = append(extraNames, c.Name)
		}
	}
	sort.Strings(gcNames)
	sort.Strings(mldNames)
	sort.Strings(extraNames)

	combos := []ClassifiedCombo{}
	for _, cb := range in.Combos {
		if !present(cb, qty) {
			continue
		}
		cc := classify(cb, commanders, names)
		cc.Tag = cascade(deckFacts{}, []ClassifiedCombo{cc})
		cc.MinBracket, cc.Borderline = cc.Tag.MinBracket()
		combos = append(combos, cc)
	}

	tag := cascade(deckFacts{
		gameChangers:   gcCount,
		extraTurnCards: extraTurnQty,
		massLandDenial: len(mldNames) > 0,
	}, combos)
	est, borderline := tag.MinBracket()

	r := Report{
		Tag:              tag,
		TagLabel:         tag.Label(),
		EstimatedBracket: est,
		Borderline:       borderline,
		TargetBracket:    in.TargetBracket,
		GameChangers:     GameChangers{Count: gcCount, Cards: gcNames},
		MassLandDenial:   mldNames,
		ExtraTurns:       extraNames,
		Combos:           combos,
		Findings:         []Finding{},
	}
	if in.TargetBracket != nil {
		r.GameChangers.Limit = GameChangerLimit(*in.TargetBracket)
		r.Findings = findings(*in.TargetBracket, r, extraTurnQty)
	}
	return r
}

// present reports whether the deck holds every ingredient in the quantity the
// combo needs (DECK-122).
func present(cb Combo, qty map[string]int) bool {
	if len(cb.Ingredients) == 0 {
		return false
	}
	for _, ing := range cb.Ingredients {
		need := ing.Quantity
		if need < 1 {
			need = 1
		}
		if qty[ing.OracleID] < need {
			return false
		}
	}
	return true
}

// classify derives the deck-dependent classification of one combo (DECK-123).
func classify(cb Combo, commanders map[string]bool, names map[string]string) ClassifiedCombo {
	cc := ClassifiedCombo{ID: cb.ID, ManaNeeded: cb.ManaNeeded, Cards: []string{}, Produces: []string{}}
	for _, f := range cb.Produces {
		switch f.Status {
		case FeatureStandalone:
			cc.Relevant = true
			cc.BorderlineRelevant = true
			cc.Produces = append(cc.Produces, f.Name)
		case FeatureContextual:
			cc.BorderlineRelevant = true
			cc.Produces = append(cc.Produces, f.Name)
		}
		if massLandDenialPattern.MatchString(f.Name) {
			cc.MassLandDenial = true
		}
		if extraTurnPattern.MatchString(f.Name) && !extraTurnForOpponentPattern.MatchString(f.Name) {
			cc.ExtraTurn = true
		}
		if strings.Contains(strings.ToLower(f.Name), "lock") {
			cc.Lock = true
		}
		if skipTurnsPattern.MatchString(f.Name) {
			cc.SkipTurns = true
		}
		if controlAllOpponentsPattern.MatchString(f.Name) {
			cc.ControlAllOpponents = true
		}
		if controlSomeOpponentsPattern.MatchString(f.Name) {
			cc.ControlSomeOpponents = true
		}
	}

	sure := 0
	arguable := 0
	if cb.HasNotablePrerequisites {
		arguable++
	}
	if !cc.BorderlineRelevant {
		arguable++
	}
	for _, ing := range cb.Ingredients {
		if name, ok := names[ing.OracleID]; ok {
			cc.Cards = append(cc.Cards, name)
		}
		if strings.Contains(ing.ZoneLocations, zoneLibrary) || commanders[ing.OracleID] {
			continue
		}
		q := ing.Quantity
		if q < 1 {
			q = 1
		}
		sure += q
	}
	sort.Strings(cc.Cards)
	cc.DefinitelyTwoCard = sure+arguable <= 2
	cc.ArguablyTwoCard = sure <= 2 && sure+arguable <= 3
	cc.Speed = speed(cb.ManaValueNeeded)
	return cc
}

// speed maps the mana a combo needs to Spellbook's 1–5 speed (5 fastest).
func speed(manaValueNeeded int) int {
	switch {
	case manaValueNeeded <= 0:
		return 5
	case manaValueNeeded <= 4:
		return 4
	case manaValueNeeded <= 6:
		return 3
	case manaValueNeeded <= 8:
		return 2
	}
	return 1
}

// deckFacts are the card-level inputs of the tag cascade.
type deckFacts struct {
	gameChangers   int
	extraTurnCards int
	massLandDenial bool
}

// cascade is Spellbook's tag rule list: the first matching tag wins.
func cascade(d deckFacts, combos []ClassifiedCombo) Tag {
	anyCombo := func(pred func(ClassifiedCombo) bool) bool {
		for _, c := range combos {
			if pred(c) {
				return true
			}
		}
		return false
	}
	switch {
	case d.gameChangers > 3 || d.extraTurnCards >= 2 || d.massLandDenial ||
		anyCombo(func(c ClassifiedCombo) bool {
			return c.ExtraTurn || c.MassLandDenial || c.ControlAllOpponents ||
				c.Speed >= 4 && c.Relevant && c.DefinitelyTwoCard
		}):
		return Ruthless
	case anyCombo(func(c ClassifiedCombo) bool {
		return c.Speed >= 4 && (c.Relevant && c.ArguablyTwoCard || c.BorderlineRelevant && c.DefinitelyTwoCard) ||
			c.Lock || c.SkipTurns || c.ControlSomeOpponents
	}):
		return Spicy
	case d.gameChangers > 0 || anyCombo(func(c ClassifiedCombo) bool {
		return c.Speed >= 3 && c.Relevant && c.DefinitelyTwoCard
	}):
		return Powerful
	case anyCombo(func(c ClassifiedCombo) bool {
		return c.Speed >= 3 && c.BorderlineRelevant && c.ArguablyTwoCard
	}):
		return Oddball
	case d.extraTurnCards > 0 || anyCombo(func(c ClassifiedCombo) bool {
		return c.Speed >= 2 && c.Relevant && c.DefinitelyTwoCard
	}):
		return Core
	}
	return Exhibition
}

// findings compares the report with the chosen bracket (DECK-125).
func findings(target int, r Report, extraTurnQty int) []Finding {
	out := []Finding{}
	if limit := GameChangerLimit(target); limit != nil && r.GameChangers.Count > *limit {
		out = append(out, Finding{
			Severity: SeverityViolation,
			Kind:     KindGameChangers,
			Message:  fmt.Sprintf("%d Game Changers; Bracket %d allows %d", r.GameChangers.Count, target, *limit),
			Cards:    r.GameChangers.Cards,
		})
	}
	if len(r.MassLandDenial) > 0 && target < 4 {
		out = append(out, Finding{
			Severity: SeverityWarning,
			Kind:     KindMassLandDenial,
			Message:  "Mass land denial is not expected below Bracket 4",
			Cards:    r.MassLandDenial,
		})
	}
	switch {
	case extraTurnQty > 0 && target == 1:
		out = append(out, Finding{
			Severity: SeverityWarning,
			Kind:     KindExtraTurn,
			Message:  "Extra-turn cards are not expected in Bracket 1",
			Cards:    r.ExtraTurns,
		})
	case extraTurnQty >= 2 && target < 4:
		out = append(out, Finding{
			Severity: SeverityWarning,
			Kind:     KindExtraTurn,
			Message:  fmt.Sprintf("%d extra-turn cards can chain; not expected below Bracket 4", extraTurnQty),
			Cards:    r.ExtraTurns,
		})
	}
	for _, c := range r.Combos {
		switch {
		case c.MinBracket > target:
			out = append(out, Finding{
				Severity: SeverityWarning,
				Kind:     KindCombo,
				Message:  fmt.Sprintf("%s combo (%s) fits Bracket %d+", c.Tag.Label(), strings.Join(c.Cards, " + "), c.MinBracket),
				ComboID:  c.ID,
				Cards:    c.Cards,
			})
		case c.Borderline && c.MinBracket == target:
			out = append(out, Finding{
				Severity: SeverityNotice,
				Kind:     KindCombo,
				Message:  fmt.Sprintf("%s combo (%s) is borderline for Bracket %d", c.Tag.Label(), strings.Join(c.Cards, " + "), target),
				ComboID:  c.ID,
				Cards:    c.Cards,
			})
		}
	}
	return out
}
