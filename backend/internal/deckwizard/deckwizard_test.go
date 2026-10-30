package deckwizard

import (
	"testing"

	"manafold-backend/internal/bracket"
)

func intp(n int) *int { return &n }

// @spec WIZ-001
func TestPolicyFor(t *testing.T) {
	cases := []struct {
		bracket int
		ok      bool
		limit   *int
	}{
		{1, true, intp(0)},
		{2, true, intp(0)},
		{3, true, intp(3)},
		{4, true, nil},
		{5, false, nil},
		{0, false, nil},
		{-1, false, nil},
		{6, false, nil},
	}
	for _, c := range cases {
		p, ok := PolicyFor(c.bracket)
		if ok != c.ok {
			t.Fatalf("PolicyFor(%d) ok = %v, want %v", c.bracket, ok, c.ok)
		}
		if !ok {
			continue
		}
		if p.Bracket != c.bracket {
			t.Errorf("PolicyFor(%d).Bracket = %d", c.bracket, p.Bracket)
		}
		switch {
		case c.limit == nil && p.GameChangerLimit != nil:
			t.Errorf("bracket %d: limit = %d, want unlimited", c.bracket, *p.GameChangerLimit)
		case c.limit != nil && (p.GameChangerLimit == nil || *p.GameChangerLimit != *c.limit):
			t.Errorf("bracket %d: limit = %v, want %d", c.bracket, p.GameChangerLimit, *c.limit)
		}
		if p.Guidance == "" {
			t.Errorf("bracket %d has no guidance", c.bracket)
		}
	}
}

// @spec WIZ-002
func TestGameChangersLeft(t *testing.T) {
	cases := []struct {
		bracket, inDeck int
		want            *int
	}{
		{1, 0, intp(0)},
		{2, 2, intp(0)}, // already over: nothing left, never negative
		{3, 0, intp(3)},
		{3, 2, intp(1)},
		{3, 5, intp(0)},
		{4, 12, nil},
	}
	for _, c := range cases {
		got := GameChangersLeft(c.bracket, c.inDeck)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("bracket %d with %d in deck: left = %d, want unlimited", c.bracket, c.inDeck, *got)
		case c.want != nil && (got == nil || *got != *c.want):
			t.Errorf("bracket %d with %d in deck: left = %v, want %d", c.bracket, c.inDeck, got, *c.want)
		}
	}
}

// @spec WIZ-003
func TestLandTarget(t *testing.T) {
	cases := []struct {
		archetype string
		want      int
	}{
		{"", 37},
		{"not_a_template", 37},
		{"midrange_value", 37},     // 36–38
		{"spellslinger", 36},       // 34–38
		{"voltron", 36},            // 35–38
		{"ramp_big_creatures", 39}, // 38–40
		{"lands_matter", 42},       // 40–44
	}
	for _, c := range cases {
		if got := LandTarget(c.archetype); got != c.want {
			t.Errorf("LandTarget(%q) = %d, want %d", c.archetype, got, c.want)
		}
	}
}

// @spec WIZ-109
func TestAppliesAtBracket(t *testing.T) {
	for b, want := range map[int]bool{-1: false, 0: false, 1: false, 2: false, 3: false, 4: true, 5: false, 6: false} {
		if got := StepFastMana.AppliesAtBracket(b); got != want {
			t.Errorf("fast mana applies at bracket %d = %v, want %v", b, got, want)
		}
	}
	for _, s := range []Step{StepFixing, StepUtility, StepPicks} {
		for b := 0; b <= 6; b++ {
			if !s.AppliesAtBracket(b) {
				t.Errorf("step %s must apply at every bracket, refused %d", s, b)
			}
		}
	}
}

// @spec WIZ-114
func TestRelaxedLandTarget(t *testing.T) {
	cases := []struct {
		name     string
		base     int
		bracket  int
		fastMana int
		want     int
	}{
		{"none held", 37, 4, 0, 37},
		{"one piece changes nothing", 37, 4, 1, 37},
		{"two pieces cost a land", 37, 4, 2, 36},
		{"three pieces still one land", 37, 4, 3, 36},
		{"six pieces", 37, 4, 6, 34},
		{"floor holds", 37, 4, 20, 32},
		{"exactly at the floor", 37, 4, 10, 32},
		{"lands-matter base relaxes from its own base", 42, 4, 4, 40},
		{"a base already under the floor is never raised", 30, 4, 10, 30},
		{"bracket 3 is the base", 37, 3, 12, 37},
		{"bracket 2 is the base", 37, 2, 12, 37},
		{"bracket 1 is the base", 37, 1, 12, 37},
		{"bracket 5 is the base", 37, 5, 12, 37},
		{"no bracket is the base", 37, 0, 12, 37},
		{"negative count is the base", 37, 4, -3, 37},
	}
	for _, c := range cases {
		if got := RelaxedLandTarget(c.base, c.bracket, c.fastMana); got != c.want {
			t.Errorf("%s: RelaxedLandTarget(%d, %d, %d) = %d, want %d", c.name, c.base, c.bracket, c.fastMana, got, c.want)
		}
	}
}

// @spec WIZ-012, WIZ-050
func TestValidLandTarget(t *testing.T) {
	for n, want := range map[int]bool{19: false, 20: true, 37: true, 60: true, 61: false, 0: false, -5: false} {
		if got := ValidLandTarget(n); got != want {
			t.Errorf("ValidLandTarget(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestParseStep(t *testing.T) {
	for in, want := range map[string]Step{"fixing": StepFixing, "utility": StepUtility, "fastmana": StepFastMana, "picks": StepPicks} {
		got, ok := ParseStep(in)
		if !ok || got != want {
			t.Errorf("ParseStep(%q) = %v, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "lands", "Fixing", "basics", "synergy", "fast_mana", "FastMana"} {
		if _, ok := ParseStep(bad); ok {
			t.Errorf("ParseStep(%q) accepted", bad)
		}
	}
	if StepFixing.Want() != 10 || StepUtility.Want() != 5 || StepPicks.Want() != 10 {
		t.Errorf("want sizes: %d %d %d", StepFixing.Want(), StepUtility.Want(), StepPicks.Want())
	}
	if StepFixing.PoolSize() != 30 || StepUtility.PoolSize() != 25 || StepPicks.PoolSize() != 40 {
		t.Errorf("pool sizes: %d %d %d", StepFixing.PoolSize(), StepUtility.PoolSize(), StepPicks.PoolSize())
	}
	if !StepFixing.IsLandStep() || !StepUtility.IsLandStep() || StepPicks.IsLandStep() {
		t.Error("IsLandStep wrong")
	}
	if StepFastMana.Want() != 8 || StepFastMana.PoolSize() != 20 || StepFastMana.IsLandStep() {
		t.Errorf("fast mana step: want %d, pool %d, land step %v", StepFastMana.Want(), StepFastMana.PoolSize(), StepFastMana.IsLandStep())
	}
	for _, s := range []Step{StepFixing, StepUtility, StepFastMana, StepPicks} {
		if s.Guidance() == "" {
			t.Errorf("step %s has no guidance", s)
		}
	}
}

func rowsByColor(rows []BasicsRow) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		out[r.Color] += r.Quantity
	}
	return out
}

func total(rows []BasicsRow) int {
	n := 0
	for _, r := range rows {
		n += r.Quantity
	}
	return n
}

// @spec WIZ-050, WIZ-051
func TestSplitBasics(t *testing.T) {
	t.Run("proportional to pips and sums to needed", func(t *testing.T) {
		rows := SplitBasics(20, []string{"U", "R"}, map[string]int{"U": 15, "R": 5})
		got := rowsByColor(rows)
		if got["U"] != 15 || got["R"] != 5 || total(rows) != 20 {
			t.Fatalf("split = %v", got)
		}
		if rows[0].Name != "Island" || rows[1].Name != "Mountain" {
			t.Errorf("names/order = %v", rows)
		}
	})
	t.Run("largest remainder, ties in WUBRG order", func(t *testing.T) {
		// 10 over 3 equal weights: 3/3/3 and the odd one goes to the first colour in WUBRG order.
		rows := SplitBasics(10, []string{"G", "W", "B"}, map[string]int{"W": 1, "B": 1, "G": 1})
		got := rowsByColor(rows)
		if got["W"] != 4 || got["B"] != 3 || got["G"] != 3 {
			t.Fatalf("split = %v", got)
		}
	})
	t.Run("a colour with no pips still gets a share", func(t *testing.T) {
		rows := SplitBasics(9, []string{"W", "G"}, map[string]int{"W": 8})
		got := rowsByColor(rows)
		if got["G"] < 1 || total(rows) != 9 {
			t.Fatalf("split = %v", got)
		}
	})
	t.Run("colourless identity uses Wastes", func(t *testing.T) {
		rows := SplitBasics(7, nil, nil)
		if len(rows) != 1 || rows[0].Name != "Wastes" || rows[0].Quantity != 7 || rows[0].Color != "C" {
			t.Fatalf("rows = %v", rows)
		}
	})
	t.Run("nothing needed", func(t *testing.T) {
		for _, n := range []int{0, -3} {
			if rows := SplitBasics(n, []string{"W"}, nil); len(rows) != 0 {
				t.Errorf("needed %d: rows = %v", n, rows)
			}
		}
	})
	t.Run("fewer basics than colours drops zero rows", func(t *testing.T) {
		rows := SplitBasics(2, []string{"W", "U", "B", "R", "G"}, map[string]int{"W": 5, "U": 4, "B": 1, "R": 1, "G": 1})
		if total(rows) != 2 {
			t.Fatalf("total = %d (%v)", total(rows), rows)
		}
		for _, r := range rows {
			if r.Quantity <= 0 {
				t.Errorf("zero row kept: %v", r)
			}
		}
	})
	t.Run("ignores colours outside the identity and is deterministic", func(t *testing.T) {
		a := SplitBasics(12, []string{"B", "R"}, map[string]int{"B": 3, "R": 3, "W": 40})
		b := SplitBasics(12, []string{"R", "B"}, map[string]int{"B": 3, "R": 3, "W": 40})
		if rowsByColor(a)["W"] != 0 || len(a) != len(b) {
			t.Fatalf("a = %v, b = %v", a, b)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("order-dependent: %v vs %v", a, b)
			}
		}
	})
}

// A two-card combo, fast (speed 4 → Spicy/Ruthless territory) or mid (speed 3 →
// Powerful), built from two oracle ids.
func comboOf(id, a, b string, manaValue int) bracket.Combo {
	return bracket.Combo{
		ID:              id,
		ManaValueNeeded: manaValue,
		Ingredients: []bracket.Ingredient{
			{OracleID: a, Quantity: 1},
			{OracleID: b, Quantity: 1},
		},
		Produces: []bracket.Feature{{Name: "Infinite mana", Status: bracket.FeatureStandalone}},
	}
}

func card(id string) bracket.Card { return bracket.Card{OracleID: id, Name: "Card " + id, Quantity: 1} }

func gc(id string) bracket.Card {
	c := card(id)
	c.GameChanger = true
	return c
}

// @spec WIZ-021
func TestAdmit_GameChangerLimit(t *testing.T) {
	base := Deck{Cards: []bracket.Card{card("a"), gc("g1")}}
	cands := []bracket.Card{gc("g2"), card("b"), gc("g3")}
	cases := []struct {
		bracket int
		want    []bool
	}{
		{1, []bool{false, true, false}},
		{2, []bool{false, true, false}},
		{3, []bool{true, true, true}}, // 1 in deck + 1 each alone ≤ 3
		{4, []bool{true, true, true}},
	}
	for _, c := range cases {
		got := Admit(base, c.bracket, cands)
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("bracket %d candidate %d: admit = %v, want %v", c.bracket, i, got[i], c.want[i])
			}
		}
	}
}

// @spec WIZ-021
func TestAdmit_DeckAlreadyOverTheLimitAdmitsNoMoreGameChangers(t *testing.T) {
	base := Deck{Cards: []bracket.Card{gc("g1"), gc("g2"), gc("g3"), gc("g4")}}
	got := Admit(base, 3, []bracket.Card{gc("g5"), card("plain")})
	if got[0] {
		t.Error("a fifth Game Changer was admitted into a Bracket 3 deck already over the limit")
	}
	if !got[1] {
		t.Error("a plain card was refused only because the deck was already over the limit")
	}
}

// @spec WIZ-021
func TestAdmit_Combos(t *testing.T) {
	// "a" is in the deck. Candidate "b" completes a fast (speed 4) two-card combo
	// with it; candidate "c" completes a speed-3 combo with it; "d" completes none.
	base := Deck{
		Cards: []bracket.Card{card("a")},
		Combos: []bracket.Combo{
			comboOf("fast", "a", "b", 3),
			comboOf("mid", "a", "c", 5),
		},
	}
	cands := []bracket.Card{card("b"), card("c"), card("d")}
	cases := []struct {
		bracket int
		want    []bool
	}{
		{2, []bool{false, false, true}},
		{3, []bool{false, true, true}}, // the fast combo reads Bracket 4+, the mid one fits 3
		{4, []bool{true, true, true}},
	}
	for _, c := range cases {
		got := Admit(base, c.bracket, cands)
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("bracket %d candidate %s: admit = %v, want %v", c.bracket, cands[i].OracleID, got[i], c.want[i])
			}
		}
	}
}

// @spec WIZ-021
func TestAdmit_MassLandDenialAndExtraTurns(t *testing.T) {
	mld := card("mld")
	mld.MassLandDenial = true
	et := card("et")
	et.ExtraTurn = true
	et2 := card("et2")
	et2.ExtraTurn = true

	base := Deck{Cards: []bracket.Card{card("a")}}
	cands := []bracket.Card{mld, et}
	cases := []struct {
		bracket int
		want    []bool
	}{
		{1, []bool{false, false}},
		{2, []bool{false, true}},
		{3, []bool{false, true}},
		{4, []bool{true, true}},
	}
	for _, c := range cases {
		got := Admit(base, c.bracket, cands)
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("bracket %d candidate %s: admit = %v, want %v", c.bracket, cands[i].OracleID, got[i], c.want[i])
			}
		}
	}

	// A second extra-turn card below Bracket 4 chains.
	withOne := Deck{Cards: []bracket.Card{card("a"), et}}
	if got := Admit(withOne, 3, []bracket.Card{et2}); got[0] {
		t.Error("second extra-turn card admitted at Bracket 3")
	}
	if got := Admit(withOne, 4, []bracket.Card{et2}); !got[0] {
		t.Error("second extra-turn card refused at Bracket 4")
	}
}

// @spec WIZ-023
func TestAdmitInOrder_CumulativeGameChangerBudget(t *testing.T) {
	base := Deck{Cards: []bracket.Card{card("a")}}
	cands := []bracket.Card{gc("g1"), gc("g2"), gc("g3"), gc("g4"), card("plain")}

	alone := Admit(base, 3, cands)
	for i := 0; i < 4; i++ {
		if !alone[i] {
			t.Fatalf("candidate %d refused alone", i)
		}
	}
	got := AdmitInOrder(base, 3, cands)
	want := []bool{true, true, true, false, true}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d: in-order admit = %v, want %v", i, got[i], want[i])
		}
	}
}

// @spec WIZ-023
func TestAdmitInOrder_PairsThatOnlyConflictTogether(t *testing.T) {
	// "b" and "c" are each fine alone at Bracket 3 with "a" in the deck? Here the
	// fast combo needs both: "b" is harmless alone, "c" is harmless alone, and the
	// pair completes a Bracket 4+ combo.
	base := Deck{
		Cards:  []bracket.Card{card("a")},
		Combos: []bracket.Combo{comboOf("pair", "b", "c", 3)},
	}
	cands := []bracket.Card{card("b"), card("c")}
	alone := Admit(base, 3, cands)
	if !alone[0] || !alone[1] {
		t.Fatalf("each should be admissible alone: %v", alone)
	}
	got := AdmitInOrder(base, 3, cands)
	if !got[0] || got[1] {
		t.Errorf("in-order = %v, want [true false]", got)
	}
}

// @spec WIZ-021
func TestAdmit_DoesNotMutateTheDeck(t *testing.T) {
	base := Deck{Cards: []bracket.Card{card("a"), card("b")}}
	_ = Admit(base, 3, []bracket.Card{gc("g")})
	_ = AdmitInOrder(base, 3, []bracket.Card{gc("g")})
	if len(base.Cards) != 2 {
		t.Errorf("deck cards were mutated: %d", len(base.Cards))
	}
}
