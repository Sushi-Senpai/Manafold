package api

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckroles"
	"manafold-backend/internal/deckwizard"
	"manafold-backend/internal/synergy"
)

// wizardRoleRawRows is how many prefiltered rows the roles query reads before
// the role classifier narrows them. Roughly one card in ten fills a given role,
// so this leaves the classifier a few hundred matches to fill a pool of 30 from.
const wizardRoleRawRows = 800

// roleCounts counts the roles of the cards on main (the commanders excluded,
// as the template-fit endpoint counts) with the same classifier the deck-vs-target
// panel uses, so a number shown here matches the builder (WIZ-070).
func (v wizardView) roleCounts() map[deckroles.Role]int {
	var entries []deckroles.Entry
	for _, e := range v.ld.entries {
		if e.Board != "main" {
			continue
		}
		entries = append(entries, deckroles.Entry{
			Card:     roleCardOf(e.TypeLine, e.OracleText, e.Keywords, e.CardFaces),
			Quantity: int(e.Quantity),
		})
	}
	return deckroles.Count(roleClassifier, entries)
}

// roleStanding is the plan's roles list: the deck's counts against its
// archetype's template, or an empty list without one (WIZ-070).
func (v wizardView) roleStanding() []deckwizard.RoleRow {
	return deckwizard.RoleStanding(v.archetype(), v.roleCounts())
}

// roleRoom finds the role's row for a roles round, or the reason the round has
// nothing to ask for: no archetype, a role already at its maximum, or a full deck
// (WIZ-072).
func (v wizardView) roleRoom(role deckroles.Role) (deckwizard.RoleRow, string) {
	rows := v.roleStanding()
	if len(rows) == 0 {
		return deckwizard.RoleRow{}, "no_archetype"
	}
	var row deckwizard.RoleRow
	for _, r := range rows {
		if r.Role == role {
			row = r
		}
	}
	switch {
	case v.mainCount >= wizardDeckSize:
		return row, "deck_full"
	case deckwizard.RoleWant(row.Count, row.Max) == 0:
		return row, "role_filled"
	}
	return row, ""
}

// fillsRole reports whether the deck-role classifier assigns the card the role.
func fillsRole(c db.Card, role deckroles.Role) bool {
	for _, got := range roleClassifier.Classify(roleCardOf(c.TypeLine, c.OracleText, c.Keywords, c.CardFaces)) {
		if got == role {
			return true
		}
	}
	return false
}

// rolePool is the roles step's raw candidate list before the price cap, tutor ban
// and bracket admission: cards of the commander's stored synergy sample that fill
// the role, in the sample's order, then the best-ranked other cards that do
// (WIZ-074). The returned view is non-nil only when a synergy card made the list,
// so the source and attribution describe what the player is shown (WIZ-076).
func (a *API) rolePool(ctx context.Context, v wizardView, role deckroles.Role, exclude []pgtype.UUID, excluded map[string]bool, limit int) ([]wizardCandidate, *synergy.CommanderView, error) {
	var out []wizardCandidate
	seen := map[string]bool{}

	fromSynergy, view, err := a.picksFromSynergy(ctx, v, excluded)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range fromSynergy {
		if len(out) < limit && fillsRole(c.card, role) {
			seen[uuidString(c.card.ID)] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		view = nil
	}

	rows, err := a.Queries.ListWizardRoleCandidates(ctx, db.ListWizardRoleCandidatesParams{
		Identity: v.identity, ExcludeIds: exclude, Pattern: deckwizard.RolePrefilter(role), Lim: wizardRoleRawRows,
	})
	if err != nil {
		return nil, nil, err
	}
	for _, c := range rows {
		if len(out) >= limit {
			break
		}
		if !seen[uuidString(c.ID)] && fillsRole(c, role) {
			out = append(out, wizardCandidate{card: c})
		}
	}
	return out, view, nil
}
