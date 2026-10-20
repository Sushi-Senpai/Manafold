package bracket_test

import (
	"testing"

	"manafold-backend/internal/bracket"
)

func ptr(i int) *int { return &i }

func card(id, name string) bracket.Card { return bracket.Card{OracleID: id, Name: name, Quantity: 1} }

func gc(id, name string) bracket.Card {
	c := card(id, name)
	c.GameChanger = true
	return c
}

func combo(id string, mana int, notable bool, features []bracket.Feature, ings ...bracket.Ingredient) bracket.Combo {
	return bracket.Combo{ID: id, ManaValueNeeded: mana, HasNotablePrerequisites: notable, Ingredients: ings, Produces: features}
}

func ing(id, zones string) bracket.Ingredient {
	return bracket.Ingredient{OracleID: id, Quantity: 1, ZoneLocations: zones}
}

var win = []bracket.Feature{{Name: "Win the game", Status: "S"}}

// @spec DECK-120, DECK-121
func TestCheck_TagCascadeFromCards(t *testing.T) {
	filler := []bracket.Card{card("a", "A"), card("b", "B")}
	manyGC := append([]bracket.Card{}, filler...)
	for _, n := range []string{"g1", "g2", "g3", "g4"} {
		manyGC = append(manyGC, gc(n, n))
	}
	mld := card("m", "Armageddon")
	mld.MassLandDenial = true
	et := func(id string) bracket.Card { c := card(id, id); c.ExtraTurn = true; return c }

	cases := []struct {
		name    string
		cards   []bracket.Card
		want    int
		wantTag bracket.Tag
	}{
		{"no flagged cards", filler, 1, bracket.Exhibition},
		{"one game changer", append(append([]bracket.Card{}, filler...), gc("g", "Rhystic Study")), 3, bracket.Powerful},
		{"three game changers", append(append([]bracket.Card{}, filler...), gc("g1", "g1"), gc("g2", "g2"), gc("g3", "g3")), 3, bracket.Powerful},
		{"four game changers", manyGC, 4, bracket.Ruthless},
		{"mass land denial", append(append([]bracket.Card{}, filler...), mld), 4, bracket.Ruthless},
		{"one extra turn", append(append([]bracket.Card{}, filler...), et("t1")), 2, bracket.Core},
		{"two extra turns", append(append([]bracket.Card{}, filler...), et("t1"), et("t2")), 4, bracket.Ruthless},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := bracket.Check(bracket.Input{Cards: c.cards})
			if r.EstimatedBracket != c.want || r.Tag != c.wantTag {
				t.Fatalf("estimate = %d (%s), want %d (%s)", r.EstimatedBracket, r.Tag, c.want, c.wantTag)
			}
			if r.EstimatedBracket > 4 {
				t.Fatalf("estimate %d exceeds 4; the check never reports Bracket 5", r.EstimatedBracket)
			}
		})
	}
}

// @spec DECK-121
func TestCheck_GameChangerCountSumsQuantity(t *testing.T) {
	c := gc("g", "Sol Ring")
	c.Quantity = 2
	r := bracket.Check(bracket.Input{Cards: []bracket.Card{c, card("x", "X")}})
	if r.GameChangers.Count != 2 {
		t.Fatalf("count = %d, want 2", r.GameChangers.Count)
	}
}

// @spec DECK-121
func TestGameChangerLimit(t *testing.T) {
	for b, want := range map[int]*int{1: ptr(0), 2: ptr(0), 3: ptr(3), 4: nil, 5: nil} {
		got := bracket.GameChangerLimit(b)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Fatalf("limit(%d) = %v, want %v", b, got, want)
		}
	}
}

// @spec DECK-122, DECK-123
func TestCheck_Combos(t *testing.T) {
	deck := []bracket.Card{card("dc", "Demonic Consultation"), card("to", "Thassa's Oracle"), card("x", "Filler")}

	t.Run("fast two-card win is Ruthless", func(t *testing.T) {
		cb := combo("742-1295", 3, false, win, ing("dc", "H"), ing("to", "H"))
		r := bracket.Check(bracket.Input{Cards: deck, Combos: []bracket.Combo{cb}})
		if r.Tag != bracket.Ruthless || r.EstimatedBracket != 4 {
			t.Fatalf("tag/bracket = %s/%d, want Ruthless/4", r.Tag, r.EstimatedBracket)
		}
		if len(r.Combos) != 1 {
			t.Fatalf("combos = %d, want 1", len(r.Combos))
		}
		c := r.Combos[0]
		if c.Speed != 4 || !c.Relevant || !c.DefinitelyTwoCard || c.Tag != bracket.Ruthless || c.MinBracket != 4 {
			t.Fatalf("combo = %+v, want speed 4, relevant, definitely two-card, Ruthless, min bracket 4", c)
		}
		if len(c.Cards) != 2 || c.Cards[0] != "Demonic Consultation" {
			t.Fatalf("cards = %v, want both names sorted", c.Cards)
		}
	})

	t.Run("notable prerequisites make it arguably two-card: Spicy", func(t *testing.T) {
		d := []bracket.Card{card("dr", "Dramatic Reversal"), card("is", "Isochron Scepter")}
		cb := combo("4821-5261", 0, true, []bracket.Feature{
			{Name: "Infinite mana", Status: "S"}, {Name: "Infinite untap", Status: "C"},
		}, ing("dr", "E"), ing("is", "B"))
		r := bracket.Check(bracket.Input{Cards: d, Combos: []bracket.Combo{cb}})
		c := r.Combos[0]
		if c.Speed != 5 || c.DefinitelyTwoCard || !c.ArguablyTwoCard {
			t.Fatalf("combo = %+v, want speed 5, arguably (not definitely) two-card", c)
		}
		if r.Tag != bracket.Spicy || r.EstimatedBracket != 3 || !r.Borderline {
			t.Fatalf("tag/bracket/borderline = %s/%d/%v, want Spicy/3/true", r.Tag, r.EstimatedBracket, r.Borderline)
		}
	})

	t.Run("slow combo does not move the deck out of Core", func(t *testing.T) {
		d := []bracket.Card{card("ba", "Blasphemous Act"), card("rp", "Repercussion")}
		cb := combo("2484-4083", 9, false, win, ing("ba", "H"), ing("rp", "B"))
		r := bracket.Check(bracket.Input{Cards: d, Combos: []bracket.Combo{cb}})
		if r.Combos[0].Speed != 1 || r.Tag != bracket.Exhibition {
			t.Fatalf("speed/tag = %d/%s, want 1/Exhibition", r.Combos[0].Speed, r.Tag)
		}
	})

	t.Run("a combo missing a card is absent", func(t *testing.T) {
		cb := combo("c", 3, false, win, ing("dc", "H"), ing("missing", "H"))
		r := bracket.Check(bracket.Input{Cards: deck, Combos: []bracket.Combo{cb}})
		if len(r.Combos) != 0 {
			t.Fatalf("combos = %d, want 0", len(r.Combos))
		}
	})

	t.Run("quantity is rechecked", func(t *testing.T) {
		two := bracket.Ingredient{OracleID: "dc", Quantity: 2, ZoneLocations: "H"}
		cb := combo("c", 3, false, win, two, ing("to", "H"))
		if r := bracket.Check(bracket.Input{Cards: deck, Combos: []bracket.Combo{cb}}); len(r.Combos) != 0 {
			t.Fatalf("combos = %d, want 0 with one copy of a two-copy ingredient", len(r.Combos))
		}
		dc := card("dc", "Demonic Consultation")
		dc.Quantity = 2
		if r := bracket.Check(bracket.Input{Cards: []bracket.Card{dc, card("to", "T")}, Combos: []bracket.Combo{cb}}); len(r.Combos) != 1 {
			t.Fatalf("combos = %d, want 1 with two copies", len(r.Combos))
		}
	})

	t.Run("library and commander ingredients are not counted", func(t *testing.T) {
		// Four ingredients, but two start in the library or are the commander,
		// so only two count: still a definite two-card combo.
		d := []bracket.Card{card("a", "A"), card("b", "B"), card("cmd", "Cmdr"), card("lib", "Lib")}
		cb := combo("c", 2, false, win, ing("a", "H"), ing("b", "B"), ing("cmd", "C"), ing("lib", "L"))
		r := bracket.Check(bracket.Input{Cards: d, Commanders: []string{"cmd"}, Combos: []bracket.Combo{cb}})
		if !r.Combos[0].DefinitelyTwoCard {
			t.Fatalf("combo = %+v, want definitely two-card", r.Combos[0])
		}
		// Without the exclusions it is a four-card combo.
		cb2 := combo("c", 2, false, win, ing("a", "H"), ing("b", "B"), ing("cmd", "H"), ing("lib", "H"))
		r = bracket.Check(bracket.Input{Cards: d, Combos: []bracket.Combo{cb2}})
		if r.Combos[0].ArguablyTwoCard {
			t.Fatalf("combo = %+v, want not two-card", r.Combos[0])
		}
	})

	t.Run("a combo with no standalone or contextual result is arguable", func(t *testing.T) {
		d := []bracket.Card{card("a", "A"), card("b", "B")}
		cb := combo("c", 2, false, []bracket.Feature{{Name: "Infinite ETB", Status: "H"}}, ing("a", "H"), ing("b", "H"))
		r := bracket.Check(bracket.Input{Cards: d, Combos: []bracket.Combo{cb}})
		c := r.Combos[0]
		if c.Relevant || c.BorderlineRelevant || c.DefinitelyTwoCard || !c.ArguablyTwoCard {
			t.Fatalf("combo = %+v, want irrelevant and arguably (not definitely) two-card", c)
		}
	})
}

// @spec DECK-123
func TestSpeedFromManaValue(t *testing.T) {
	d := []bracket.Card{card("a", "A"), card("b", "B")}
	for mv, want := range map[int]int{0: 5, 1: 4, 4: 4, 5: 3, 6: 3, 7: 2, 8: 2, 9: 1, 20: 1} {
		cb := combo("c", mv, false, win, ing("a", "H"), ing("b", "H"))
		r := bracket.Check(bracket.Input{Cards: d, Combos: []bracket.Combo{cb}})
		if r.Combos[0].Speed != want {
			t.Fatalf("speed(mv=%d) = %d, want %d", mv, r.Combos[0].Speed, want)
		}
	}
}

// @spec DECK-124
func TestCheck_ListsMassLandDenialAndExtraTurnCards(t *testing.T) {
	arm := card("m", "Armageddon")
	arm.MassLandDenial = true
	tw := card("t", "Time Warp")
	tw.ExtraTurn = true
	r := bracket.Check(bracket.Input{Cards: []bracket.Card{arm, tw, card("x", "X")}})
	if len(r.MassLandDenial) != 1 || r.MassLandDenial[0] != "Armageddon" || len(r.ExtraTurns) != 1 || r.ExtraTurns[0] != "Time Warp" {
		t.Fatalf("mld/extra turns = %v / %v", r.MassLandDenial, r.ExtraTurns)
	}
}

func severities(r bracket.Report, kind string) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Kind == kind {
			out = append(out, f.Severity)
		}
	}
	return out
}

// @spec DECK-125
func TestCheck_FindingsAgainstChosenBracket(t *testing.T) {
	four := []bracket.Card{gc("1", "1"), gc("2", "2"), gc("3", "3"), gc("4", "4")}
	one := []bracket.Card{gc("1", "1")}

	t.Run("no chosen bracket, no findings", func(t *testing.T) {
		r := bracket.Check(bracket.Input{Cards: four})
		if len(r.Findings) != 0 || r.GameChangers.Limit != nil {
			t.Fatalf("findings = %v, limit = %v; want none", r.Findings, r.GameChangers.Limit)
		}
	})
	t.Run("game changer limits", func(t *testing.T) {
		for _, c := range []struct {
			cards  []bracket.Card
			target int
			want   int // violation count
		}{
			{one, 1, 1}, {one, 2, 1}, {one, 3, 0}, {four, 3, 1}, {four, 4, 0}, {four, 5, 0}, {nil, 1, 0},
		} {
			r := bracket.Check(bracket.Input{Cards: c.cards, TargetBracket: ptr(c.target)})
			if got := len(severities(r, bracket.KindGameChangers)); got != c.want {
				t.Fatalf("%d game changers vs bracket %d: %d violations, want %d", len(c.cards), c.target, got, c.want)
			}
		}
	})
	t.Run("mass land denial warns below 4", func(t *testing.T) {
		arm := card("m", "Armageddon")
		arm.MassLandDenial = true
		for target, want := range map[int]int{1: 1, 3: 1, 4: 0, 5: 0} {
			r := bracket.Check(bracket.Input{Cards: []bracket.Card{arm}, TargetBracket: ptr(target)})
			if got := severities(r, bracket.KindMassLandDenial); len(got) != want || (want == 1 && got[0] != bracket.SeverityWarning) {
				t.Fatalf("bracket %d: %v, want %d warning(s)", target, got, want)
			}
		}
	})
	t.Run("extra turns", func(t *testing.T) {
		tw := card("t", "Time Warp")
		tw.ExtraTurn = true
		tw2 := card("t2", "Temporal Manipulation")
		tw2.ExtraTurn = true
		for _, c := range []struct {
			cards  []bracket.Card
			target int
			want   int
		}{
			{[]bracket.Card{tw}, 1, 1}, {[]bracket.Card{tw}, 2, 0}, {[]bracket.Card{tw}, 3, 0},
			{[]bracket.Card{tw, tw2}, 2, 1}, {[]bracket.Card{tw, tw2}, 3, 1}, {[]bracket.Card{tw, tw2}, 4, 0},
		} {
			r := bracket.Check(bracket.Input{Cards: c.cards, TargetBracket: ptr(c.target)})
			if got := len(severities(r, bracket.KindExtraTurn)); got != c.want {
				t.Fatalf("%d extra-turn cards vs bracket %d: %d findings, want %d", len(c.cards), c.target, got, c.want)
			}
		}
	})
	t.Run("combos warn above and notice at the bracket", func(t *testing.T) {
		d := []bracket.Card{card("dc", "DC"), card("to", "TO"), card("dr", "DR"), card("is", "IS")}
		ruthless := combo("r", 3, false, win, ing("dc", "H"), ing("to", "H"))
		spicy := combo("s", 0, true, []bracket.Feature{{Name: "Infinite mana", Status: "S"}}, ing("dr", "E"), ing("is", "B"))
		in := bracket.Input{Cards: d, Combos: []bracket.Combo{ruthless, spicy}}

		in.TargetBracket = ptr(2)
		got := severities(bracket.Check(in), bracket.KindCombo)
		if len(got) != 2 || got[0] != bracket.SeverityWarning || got[1] != bracket.SeverityWarning {
			t.Fatalf("bracket 2: %v, want two warnings", got)
		}
		in.TargetBracket = ptr(3)
		got = severities(bracket.Check(in), bracket.KindCombo)
		if len(got) != 2 || got[0] != bracket.SeverityWarning || got[1] != bracket.SeverityNotice {
			t.Fatalf("bracket 3: %v, want a warning for the Ruthless combo and a notice for the Spicy one", got)
		}
		in.TargetBracket = ptr(4)
		if got := severities(bracket.Check(in), bracket.KindCombo); len(got) != 0 {
			t.Fatalf("bracket 4: %v, want none", got)
		}
	})
}
