package deckwizard

import (
	"strings"
	"testing"

	"manafold-backend/internal/deckroles"
	"manafold-backend/internal/decktemplates"
)

// @spec WIZ-071
func TestParseRole(t *testing.T) {
	for _, r := range deckroles.Roles {
		got, ok := ParseRole(string(r))
		if !ok || got != r {
			t.Errorf("ParseRole(%q) = %q, %v", r, got, ok)
		}
	}
	for _, bad := range []string{"", "Ramp", "removal", "land", " ramp"} {
		if _, ok := ParseRole(bad); ok {
			t.Errorf("ParseRole(%q) accepted", bad)
		}
	}
}

// @spec WIZ-071, WIZ-073, WIZ-074
func TestRolesStepShape(t *testing.T) {
	s, ok := ParseStep("roles")
	if !ok || s != StepRoles {
		t.Fatalf("ParseStep(roles) = %q, %v", s, ok)
	}
	if s.IsLandStep() {
		t.Error("roles must not be a land step")
	}
	if s.Want() != 8 || s.PoolSize() != 30 {
		t.Errorf("roles want/pool = %d/%d, want 8/30", s.Want(), s.PoolSize())
	}
}

// @spec WIZ-073
func TestRoleWant(t *testing.T) {
	cases := []struct{ count, max, want int }{
		{0, 12, 8},  // plenty of room: the step's cap
		{4, 12, 8},  // exactly the cap
		{5, 12, 7},  // room under the cap
		{11, 12, 1}, // one slot left
		{12, 12, 0}, // at the maximum
		{15, 12, 0}, // over it: never negative
	}
	for _, c := range cases {
		if got := RoleWant(c.count, c.max); got != c.want {
			t.Errorf("RoleWant(%d, %d) = %d, want %d", c.count, c.max, got, c.want)
		}
	}
}

// @spec WIZ-070
func TestRoleStanding(t *testing.T) {
	counts := map[deckroles.Role]int{
		deckroles.Ramp:            4,
		deckroles.CardDraw:        11,
		deckroles.TargetedRemoval: 14,
		// board wipe absent: counts as 0
	}
	rows := RoleStanding("midrange_value", counts)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	want := []struct {
		role     deckroles.Role
		count    int
		min, max int
		status   decktemplates.Status
	}{
		{deckroles.Ramp, 4, 10, 12, decktemplates.StatusUnder},
		{deckroles.CardDraw, 11, 10, 12, decktemplates.StatusOK},
		{deckroles.TargetedRemoval, 14, 8, 10, decktemplates.StatusOver},
		{deckroles.BoardWipe, 0, 2, 4, decktemplates.StatusUnder},
	}
	for i, w := range want {
		r := rows[i]
		if r.Role != w.role || r.Count != w.count || r.Min != w.min || r.Max != w.max || r.Status != w.status {
			t.Errorf("row %d = %+v, want %+v", i, r, w)
		}
		if r.Label == "" {
			t.Errorf("row %d has no label", i)
		}
	}
}

// @spec WIZ-070
func TestRoleStandingUsesArchetypeRange(t *testing.T) {
	// Control runs 4–6 wipes where every other archetype runs 2–4.
	rows := RoleStanding("control", nil)
	if rows[3].Role != deckroles.BoardWipe || rows[3].Min != 4 || rows[3].Max != 6 {
		t.Errorf("control wipe row = %+v", rows[3])
	}
}

// @spec WIZ-070
func TestRoleStandingWithoutArchetype(t *testing.T) {
	for _, key := range []string{"", "no_such_archetype"} {
		rows := RoleStanding(key, map[deckroles.Role]int{deckroles.Ramp: 3})
		if rows == nil || len(rows) != 0 {
			t.Errorf("RoleStanding(%q) = %v, want a non-nil empty list", key, rows)
		}
	}
}

// @spec WIZ-076
func TestRoleGuidanceNamesRoleCountAndRange(t *testing.T) {
	g := RoleGuidance(deckroles.Ramp, 3, 10, 12)
	for _, want := range []string{"ramp", "3", "10", "12"} {
		if !strings.Contains(strings.ToLower(g), want) {
			t.Errorf("guidance %q lacks %q", g, want)
		}
	}
	for _, r := range deckroles.Roles {
		if RoleGuidance(r, 0, 1, 2) == "" {
			t.Errorf("no guidance for %s", r)
		}
	}
}

// @spec WIZ-074
func TestRolePrefilterCoversClassifier(t *testing.T) {
	// The prefilter is a coarse SQL regex; the Go classifier decides. What the
	// prefilter must never do is exclude a card the classifier would accept, so
	// each role's own positive examples must match its pattern (Go's regexp is a
	// close enough stand-in for Postgres's ARE on these simple alternations).
	examples := map[deckroles.Role][]deckroles.Card{
		deckroles.Ramp: {
			{TypeLine: "Artifact", OracleText: "{T}: Add {C}{C}."},
			{TypeLine: "Sorcery", OracleText: "Search your library for a basic land card, put it onto the battlefield tapped, then shuffle."},
			{TypeLine: "Instant", OracleText: "Create a Treasure token."},
			{TypeLine: "Enchantment", OracleText: "You may play an additional land on each of your turns."},
		},
		deckroles.CardDraw: {
			{TypeLine: "Sorcery", OracleText: "Draw three cards."},
		},
		deckroles.TargetedRemoval: {
			{TypeLine: "Instant", OracleText: "Destroy target creature."},
			{TypeLine: "Instant", OracleText: "Exile target artifact or enchantment."},
			{TypeLine: "Instant", OracleText: "Deals 3 damage to any target."},
			{TypeLine: "Instant", OracleText: "Return target creature to its owner's hand."},
			{TypeLine: "Instant", OracleText: "Target creature gets -3/-3 until end of turn."},
		},
		deckroles.BoardWipe: {
			{TypeLine: "Sorcery", OracleText: "Destroy all creatures."},
			{TypeLine: "Sorcery", OracleText: "Exile each nonland permanent."},
			{TypeLine: "Sorcery", OracleText: "Return all creatures to their owners' hands."},
			{TypeLine: "Sorcery", OracleText: "Deals 3 damage to each creature."},
			{TypeLine: "Instant", OracleText: "All creatures get -2/-2 until end of turn."},
		},
	}
	cl := deckroles.OracleClassifier{}
	for role, cards := range examples {
		re := compilePrefilter(t, role)
		for _, c := range cards {
			has := false
			for _, got := range cl.Classify(c) {
				has = has || got == role
			}
			if !has {
				t.Fatalf("test data: classifier does not give %q the role %s", c.OracleText, role)
			}
			if !re.MatchString(c.OracleText) {
				t.Errorf("%s prefilter %q rejects %q", role, RolePrefilter(role), c.OracleText)
			}
		}
	}
}

func TestRolePrefilterUnknownRole(t *testing.T) {
	if RolePrefilter("nonsense") != "" {
		t.Error("unknown role should have no prefilter")
	}
}
