package decktemplates

import (
	"testing"

	"manafold-backend/internal/deckroles"
)

func rowOf(t *testing.T, tpl Template, key string) Row {
	t.Helper()
	for _, r := range tpl.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("template %s has no %s row", tpl.Key, key)
	return Row{}
}

// @spec DECK-100
func TestTemplates_ApprovedTable(t *testing.T) {
	want := map[string][5][2]int{ // lands, creatures, instants & sorceries, artifacts, enchantments
		"go_wide_tribal":     {{36, 38}, {28, 35}, {10, 14}, {8, 11}, {4, 8}},
		"voltron":            {{35, 38}, {18, 24}, {10, 14}, {14, 20}, {6, 12}},
		"spellslinger":       {{34, 38}, {10, 16}, {28, 36}, {8, 12}, {2, 6}},
		"midrange_value":     {{36, 38}, {25, 32}, {14, 20}, {8, 12}, {4, 8}},
		"control":            {{36, 38}, {12, 18}, {25, 32}, {8, 12}, {4, 8}},
		"ramp_big_creatures": {{38, 40}, {22, 28}, {12, 18}, {10, 14}, {3, 7}},
		"artifacts":          {{34, 38}, {12, 20}, {8, 14}, {28, 36}, {0, 4}},
		"enchantress":        {{36, 38}, {12, 18}, {10, 14}, {6, 10}, {24, 32}},
		"aristocrats":        {{36, 38}, {30, 38}, {10, 14}, {8, 12}, {4, 8}},
		"lands_matter":       {{40, 44}, {18, 24}, {14, 18}, {6, 10}, {4, 8}},
	}
	all := All()
	if len(all) != len(want) {
		t.Fatalf("got %d templates, want %d", len(all), len(want))
	}
	typeKeys := []string{RowLands, RowCreatures, RowInstantsSorceries, RowArtifacts, RowEnchantments}
	for _, tpl := range all {
		w, ok := want[tpl.Key]
		if !ok {
			t.Errorf("unexpected template %q", tpl.Key)
			continue
		}
		for i, k := range typeKeys {
			r := rowOf(t, tpl, k)
			if r.Kind != KindType || r.Min != w[i][0] || r.Max != w[i][1] {
				t.Errorf("%s %s = %s %d–%d, want type %d–%d", tpl.Key, k, r.Kind, r.Min, r.Max, w[i][0], w[i][1])
			}
		}
	}
}

// @spec DECK-101
func TestTemplates_LandRule(t *testing.T) {
	for _, tpl := range All() {
		r := rowOf(t, tpl, RowLands)
		if r.Max < 38 || r.Min < 34 {
			t.Errorf("%s lands %d–%d breaks the land rule (min ≥ 34, max ≥ 38)", tpl.Key, r.Min, r.Max)
		}
		if r.RelaxedFromBracket != 4 {
			t.Errorf("%s lands relaxed_from_bracket = %d, want 4", tpl.Key, r.RelaxedFromBracket)
		}
	}
}

// @spec DECK-102
func TestTemplates_RoleTargets(t *testing.T) {
	for _, tpl := range All() {
		wipes := [2]int{2, 4}
		if tpl.Key == "control" {
			wipes = [2]int{4, 6}
		}
		for key, want := range map[string][2]int{
			RowRamp: {10, 12}, RowCardDraw: {10, 12}, RowTargetedRemoval: {8, 10}, RowBoardWipes: wipes,
		} {
			r := rowOf(t, tpl, key)
			if r.Kind != KindRole || r.Min != want[0] || r.Max != want[1] {
				t.Errorf("%s %s = %s %d–%d, want role %d–%d", tpl.Key, key, r.Kind, r.Min, r.Max, want[0], want[1])
			}
			if r.RelaxedFromBracket != 0 {
				t.Errorf("%s %s carries a bracket relaxation", tpl.Key, key)
			}
		}
	}
}

// @spec DECK-100
func TestAll_ReturnsCopy(t *testing.T) {
	a := All()
	a[0].Rows[0].Min = -1
	if All()[0].Rows[0].Min == -1 {
		t.Fatal("All exposes the authoritative rows to mutation")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup found an unknown key")
	}
}

// @spec DECK-105
func TestCounts_MapsTypesAndRoles(t *testing.T) {
	got := Counts(
		map[string]int{"Land": 37, "Creature": 20, "Instant": 6, "Sorcery": 4, "Artifact": 9, "Enchantment": 5, "Planeswalker": 2, "Battle": 1},
		map[deckroles.Role]int{deckroles.Ramp: 11, deckroles.BoardWipe: 3},
	)
	want := map[string]int{
		RowLands: 37, RowCreatures: 20, RowInstantsSorceries: 10, RowArtifacts: 9, RowEnchantments: 5,
		RowRamp: 11, RowBoardWipes: 3,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("counts carry extra keys: %v", got)
	}
}

// @spec DECK-104, DECK-108
func TestEvaluate_StatusAndBracketRelaxation(t *testing.T) {
	tpl, ok := Lookup("midrange_value")
	if !ok {
		t.Fatal("midrange_value missing")
	}
	statusOf := func(fits []RowFit, key string) (int, Status) {
		for _, f := range fits {
			if f.Key == key {
				return f.Count, f.Status
			}
		}
		t.Fatalf("no fit row %s", key)
		return 0, ""
	}

	counts := map[string]int{RowLands: 36, RowCreatures: 33, RowInstantsSorceries: 13, RowRamp: 12}
	fits := Evaluate(tpl, counts, 0)
	if len(fits) != len(tpl.Rows) {
		t.Fatalf("got %d rows, want %d", len(fits), len(tpl.Rows))
	}
	for key, want := range map[string]Status{
		RowLands:             StatusOK,    // inclusive min
		RowCreatures:         StatusOver,  // 33 > 32
		RowInstantsSorceries: StatusUnder, // 13 < 14
		RowRamp:              StatusOK,    // inclusive max
		RowBoardWipes:        StatusUnder, // missing counts as 0
	} {
		if _, got := statusOf(fits, key); got != want {
			t.Errorf("%s status = %s, want %s", key, got, want)
		}
	}

	low := map[string]int{RowLands: 30, RowCreatures: 10}
	for _, tc := range []struct {
		bracket int
		lands   Status
	}{{0, StatusUnder}, {3, StatusUnder}, {4, StatusRelaxed}, {5, StatusRelaxed}} {
		fits := Evaluate(tpl, low, tc.bracket)
		if n, got := statusOf(fits, RowLands); got != tc.lands || n != 30 {
			t.Errorf("bracket %d: lands = %d %s, want 30 %s", tc.bracket, n, got, tc.lands)
		}
		if _, got := statusOf(fits, RowCreatures); got != StatusUnder {
			t.Errorf("bracket %d: creatures relaxed too (%s)", tc.bracket, got)
		}
	}
	if _, got := statusOf(Evaluate(tpl, map[string]int{RowLands: 45}, 4), RowLands); got != StatusOver {
		t.Errorf("bracket 4 over-max lands = %s, want over", got)
	}
}
