package deckwizard

import (
	"fmt"

	"manafold-backend/internal/deckroles"
	"manafold-backend/internal/decktemplates"
)

// roleWantCap is the most cards one roles round asks the model for (WIZ-073).
const roleWantCap = 8

// ParseRole reads a propose request's role name (WIZ-071).
func ParseRole(s string) (deckroles.Role, bool) {
	for _, r := range deckroles.Roles {
		if string(r) == s {
			return r, true
		}
	}
	return "", false
}

// RoleWant is how many cards a roles round may ask for: the step's cap, limited
// to the room left under the template's maximum for the role (WIZ-073). Zero
// means the deck already has as many of the role as its template wants.
func RoleWant(count, ceiling int) int {
	return min(roleWantCap, max(0, ceiling-count))
}

// RoleRow is one role's standing against the chosen archetype's template.
type RoleRow struct {
	Role   deckroles.Role       `json:"role"`
	Label  string               `json:"label"`
	Count  int                  `json:"count"`
	Min    int                  `json:"min"`
	Max    int                  `json:"max"`
	Status decktemplates.Status `json:"status"`
}

// RoleStanding reads the deck's role counts against the archetype's template,
// one row per role in deckroles.Roles order. It returns an empty, non-nil list
// when the archetype is unknown or unset, because there is no template to read
// against (WIZ-070).
func RoleStanding(archetype string, counts map[deckroles.Role]int) []RoleRow {
	rows := []RoleRow{}
	tpl, ok := decktemplates.Lookup(archetype)
	if !ok {
		return rows
	}
	byKey := make(map[string]int, len(counts))
	for r, n := range counts {
		byKey[string(r)] = n
	}
	fits := map[string]decktemplates.RowFit{}
	for _, f := range decktemplates.Evaluate(tpl, byKey, 0) {
		fits[f.Key] = f
	}
	for _, role := range deckroles.Roles {
		f, ok := fits[string(role)]
		if !ok {
			continue
		}
		rows = append(rows, RoleRow{Role: role, Label: f.Label, Count: f.Count, Min: f.Min, Max: f.Max, Status: f.Status})
	}
	return rows
}

// roleNoun and roleFocus describe each role to the model.
var roleNoun = map[deckroles.Role]string{
	deckroles.Ramp:            "ramp (mana rocks, mana dorks, land fetching and extra land drops)",
	deckroles.CardDraw:        "card draw",
	deckroles.TargetedRemoval: "targeted removal",
	deckroles.BoardWipe:       "board wipes",
}

var roleFocus = map[deckroles.Role]string{
	deckroles.Ramp: "Favour cheap, efficient ramp, and mix mana rocks with land-fetching so the deck does not " +
		"rely on one kind.",
	deckroles.CardDraw: "Favour repeatable or low-cost draw that suits the commander's plan over clunky one-shots.",
	deckroles.TargetedRemoval: "Mix cheap spot removal with flexible answers that handle more than creatures, " +
		"and favour instant speed.",
	deckroles.BoardWipe: "Board wipes hurt a deck that wins through its own board, so favour the ones that " +
		"fit this commander's plan (one-sided, selective, or ones the deck can rebuild from).",
}

// RoleGuidance is the roles step's instruction to the model: which role to fill,
// where the deck stands against its template, and what to favour (WIZ-076). It
// steers curation; the pool it curates from is what binds.
func RoleGuidance(role deckroles.Role, count, lo, hi int) string {
	return fmt.Sprintf("Choose cards that fill the deck's %s slots. The deck has %d such cards and its archetype "+
		"template wants %d to %d. Pick the best fits for this commander, strategy and bracket. %s",
		roleNoun[role], count, lo, hi, roleFocus[role])
}

// RolePrefilter is a coarse, case-insensitive regular expression that every
// card the role classifier could assign the role matches in its Oracle text (or
// its faces' text). The roles query uses it only to bound the rows read; the Go
// classifier alone decides whether a card fills the role, so a loose pattern
// costs rows, never correctness (WIZ-074). Empty for an unknown role.
func RolePrefilter(role deckroles.Role) string {
	switch role {
	case deckroles.Ramp:
		return `add |land|treasure`
	case deckroles.CardDraw:
		return `draw`
	case deckroles.TargetedRemoval:
		return `destroy|exile|damage|return|gets? -`
	case deckroles.BoardWipe:
		return `destroy|exile|damage|return|gets? -`
	}
	return ""
}
