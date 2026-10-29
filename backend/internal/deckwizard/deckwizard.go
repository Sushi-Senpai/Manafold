// Package deckwizard is the deterministic half of Manafold's interactive deck
// wizard: the bracket policy, the land target, the basic-land split, and the
// bracket admission check that decides which cards the wizard may put in front
// of the player. It is a pure function of plain structs — no database, no AI —
// like deckrules, deckstats and bracket; the handler in internal/api assembles
// the inputs from the mirror and the deck.
//
// The wizard's rules are filters, not prompts: whatever this package refuses is
// never offered to the language model, and whatever the model names is checked
// against it again. Nothing here writes a deck.
//
// @spec WIZ-001, WIZ-003, WIZ-021, WIZ-023, WIZ-050
package deckwizard

import (
	"fmt"
	"sort"
	"strings"

	"manafold-backend/internal/bracket"
	"manafold-backend/internal/decktemplates"
)

// Step is one curated round of the wizard.
type Step string

const (
	StepFixing  Step = "fixing"
	StepUtility Step = "utility"
	StepPicks   Step = "picks"
)

// ParseStep reads a propose request's step name.
func ParseStep(s string) (Step, bool) {
	switch Step(s) {
	case StepFixing, StepUtility, StepPicks:
		return Step(s), true
	}
	return "", false
}

// Want is the most proposals one round returns (WIZ-015).
func (s Step) Want() int {
	if s == StepUtility {
		return 5
	}
	return 10
}

// PoolSize is how many candidates one round shows the model (WIZ-040, WIZ-041,
// WIZ-043).
func (s Step) PoolSize() int {
	switch s {
	case StepFixing:
		return 30
	case StepUtility:
		return 25
	}
	return 40
}

// IsLandStep reports whether the step proposes lands.
func (s Step) IsLandStep() bool { return s == StepFixing || s == StepUtility }

// Guidance is the step's instruction to the model. It steers curation; the pool
// it curates from is what binds.
func (s Step) Guidance() string {
	switch s {
	case StepFixing:
		return "Choose the nonbasic lands that best fix this deck's colours. Weigh how many colours " +
			"the deck needs, whether a land enters tapped, and what it costs; prefer lands that " +
			"enter untapped or ease the colour requirements of the commander."
	case StepUtility:
		return "Choose utility lands that do something useful for this commander and strategy " +
			"beyond making mana. Remember each colourless land makes the deck's colours less " +
			"consistent, so pick only the ones that earn their slot."
	}
	return "Choose the cards that best support this commander's strategy and the player's notes. " +
		"Prefer cards with high synergy points when they are shown, mix roles rather than stacking " +
		"one effect, and use Game Changers sparingly and only where they clearly pay off."
}

// Policy is what a bracket permits the wizard to propose.
type Policy struct {
	Bracket int
	// GameChangerLimit is the most Game Changers the bracket allows; nil means
	// unlimited.
	GameChangerLimit *int
	// Guidance steers the model. The admission check below is what binds.
	Guidance string
}

// PolicyFor returns the policy for Brackets 1–4. Bracket 5 (cEDH is a metagame,
// not a card list) and anything else is not supported (WIZ-001).
func PolicyFor(b int) (Policy, bool) {
	var guidance string
	switch b {
	case 1:
		guidance = "Bracket 1 (Exhibition): theme and fun over power. No Game Changers, no combos, " +
			"no extra turns, no mass land denial; avoid tutors and anything that wins early."
	case 2:
		guidance = "Bracket 2 (Core): precon-level power. No Game Changers, no infinite combos, no " +
			"mass land denial; extra turns only if incidental."
	case 3:
		guidance = "Bracket 3 (Upgraded): stronger than a precon but not optimised. Up to three Game " +
			"Changers, so spend them sparingly; some combos are fine as long as they cannot win the " +
			"game quickly; no mass land denial."
	case 4:
		guidance = "Bracket 4 (Optimized): high power. Game Changers, fast mana, efficient interaction, " +
			"tutors and strong combos are all expected where they fit the commander's plan."
	default:
		return Policy{}, false
	}
	return Policy{Bracket: b, GameChangerLimit: bracket.GameChangerLimit(b), Guidance: guidance}, true
}

// GameChangersLeft is how many more Game Changers a bracket admits given how
// many the deck holds: nil for unlimited, never negative.
func GameChangersLeft(b, inDeck int) *int {
	limit := bracket.GameChangerLimit(b)
	if limit == nil {
		return nil
	}
	left := *limit - inDeck
	if left < 0 {
		left = 0
	}
	return &left
}

// Land target bounds and default (WIZ-003, WIZ-012).
const (
	DefaultLandTarget = 37
	MinLandTarget     = 20
	MaxLandTarget     = 60
)

// ValidLandTarget reports whether a player-supplied land target is in range.
func ValidLandTarget(n int) bool { return n >= MinLandTarget && n <= MaxLandTarget }

// LandTarget is the floor of the midpoint of the archetype's land range, or
// DefaultLandTarget with no (or an unknown) archetype. The Bracket 4 relaxation
// is deliberately not applied: the wizard cannot yet tell whether the deck will
// run heavy fast mana (WIZ-003).
func LandTarget(archetype string) int {
	tpl, ok := decktemplates.Lookup(archetype)
	if !ok {
		return DefaultLandTarget
	}
	for _, r := range tpl.Rows {
		if r.Key == decktemplates.RowLands {
			return (r.Min + r.Max) / 2
		}
	}
	return DefaultLandTarget
}

// BasicsRow is one line of the basic-land plan.
type BasicsRow struct {
	Name     string // the basic land card's name
	Color    string // W U B R G, or C for Wastes
	Quantity int
}

var wubrg = []string{"W", "U", "B", "R", "G"}

var basicLandNames = map[string]string{
	"W": "Plains", "U": "Island", "B": "Swamp", "R": "Mountain", "G": "Forest", "C": "Wastes",
}

// SplitBasics divides `needed` basic lands across the identity's colours by
// largest-remainder allocation over weights max(pips, 1); ties go to the colour
// earlier in W-U-B-R-G order. A colourless identity gets Wastes. Colours outside
// the identity are ignored and zero rows are dropped (WIZ-050, WIZ-051).
func SplitBasics(needed int, identity []string, pips map[string]int) []BasicsRow {
	if needed <= 0 {
		return nil
	}
	in := map[string]bool{}
	for _, c := range identity {
		in[strings.ToUpper(c)] = true
	}
	var colors []string
	for _, c := range wubrg {
		if in[c] {
			colors = append(colors, c)
		}
	}
	if len(colors) == 0 {
		return []BasicsRow{{Name: basicLandNames["C"], Color: "C", Quantity: needed}}
	}

	weights := make([]int, len(colors))
	sum := 0
	for i, c := range colors {
		w := pips[c]
		if w < 1 {
			w = 1
		}
		weights[i] = w
		sum += w
	}

	type share struct {
		idx, base, rem int
	}
	shares := make([]share, len(colors))
	assigned := 0
	for i := range colors {
		prod := needed * weights[i]
		shares[i] = share{idx: i, base: prod / sum, rem: prod % sum}
		assigned += shares[i].base
	}
	order := append([]share(nil), shares...)
	sort.SliceStable(order, func(a, b int) bool { return order[a].rem > order[b].rem })
	for k := 0; k < needed-assigned; k++ {
		shares[order[k].idx].base++
	}

	var rows []BasicsRow
	for i, c := range colors {
		if shares[i].base > 0 {
			rows = append(rows, BasicsRow{Name: basicLandNames[c], Color: c, Quantity: shares[i].base})
		}
	}
	return rows
}

// Deck is the bracket-relevant view of the working deck: its main and command
// cards, the oracle ids on the command board, and the Spellbook combos whose
// card ingredients all lie within the deck plus the candidates under test.
type Deck struct {
	Cards      []bracket.Card
	Commanders []string
	Combos     []bracket.Combo
}

// Admit reports, for each candidate alone, whether the bracket admits adding it
// to the deck (WIZ-021, WIZ-022). The deck is not modified.
func Admit(d Deck, target int, cands []bracket.Card) []bool {
	base := bracket.Check(input(d, target, nil))
	baseWarnings := warningKeys(base)
	out := make([]bool, len(cands))
	for i := range cands {
		out[i] = admits(d, target, base.GameChangers.Count, baseWarnings, cands[i])
	}
	return out
}

// AdmitInOrder tests the candidates one after another, each admitted card
// joining the working deck before the next is tested, so that accepting any
// subset of the admitted cards keeps the deck inside the bracket (WIZ-023). The
// caller's deck is not modified.
func AdmitInOrder(d Deck, target int, cands []bracket.Card) []bool {
	work := Deck{
		Cards:      append([]bracket.Card(nil), d.Cards...),
		Commanders: d.Commanders,
		Combos:     d.Combos,
	}
	out := make([]bool, len(cands))
	for i := range cands {
		base := bracket.Check(input(work, target, nil))
		if admits(work, target, base.GameChangers.Count, warningKeys(base), cands[i]) {
			out[i] = true
			work.Cards = append(work.Cards, cands[i])
		}
	}
	return out
}

func input(d Deck, target int, extra *bracket.Card) bracket.Input {
	cards := d.Cards
	if extra != nil {
		cards = append(append([]bracket.Card(nil), d.Cards...), *extra)
	}
	return bracket.Input{Cards: cards, Commanders: d.Commanders, Combos: d.Combos, TargetBracket: &target}
}

// admits applies the two admission rules to one candidate against a deck whose
// own report is already known.
func admits(d Deck, target, gcInDeck int, baseWarnings map[string]bool, c bracket.Card) bool {
	qty := c.Quantity
	if qty < 1 {
		qty = 1
	}
	if c.GameChanger {
		if limit := bracket.GameChangerLimit(target); limit != nil && gcInDeck+qty > *limit {
			return false
		}
	}
	with := bracket.Check(input(d, target, &c))
	for key := range warningKeys(with) {
		if !baseWarnings[key] {
			return false
		}
	}
	return true
}

// warningKeys identifies each warning finding so that a warning the deck
// already had can be told from one a candidate adds.
func warningKeys(r bracket.Report) map[string]bool {
	keys := map[string]bool{}
	for _, f := range r.Findings {
		if f.Severity != bracket.SeverityWarning {
			continue
		}
		keys[fmt.Sprintf("%s|%s|%s|%s|%s", f.Severity, f.Kind, f.ComboID, f.Message, strings.Join(f.Cards, ","))] = true
	}
	return keys
}
