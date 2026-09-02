package cardsync

import "testing"

// @spec CARD-003
func TestDeriveSingletonLimit(t *testing.T) {
	cases := []struct {
		name       string
		oracleText string
		wantValid  bool
		wantValue  int32
	}{
		{"any number clause", "A deck can have any number of cards named Persistent Petitioners.", true, 0},
		{"up to spelled number", "A deck can have up to seven cards named Seven Dwarves.", true, 7},
		{"up to numeric", "A deck can have up to 3 cards named Test.", true, 3},
		{"plain singleton card", "Destroy target creature.", false, 0},
		{"up to N but not deck-limit wording", "Return up to two target creatures to their owners' hands.", false, 0},
		{"empty oracle text", "", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveSingletonLimit(tc.oracleText)
			if got.Valid != tc.wantValid {
				t.Fatalf("Valid = %v, want %v (text %q)", got.Valid, tc.wantValid, tc.oracleText)
			}
			if tc.wantValid && got.Int32 != tc.wantValue {
				t.Fatalf("Int32 = %d, want %d", got.Int32, tc.wantValue)
			}
		})
	}
}

// @spec CARD-004
func TestDeriveCanBeCommander(t *testing.T) {
	cases := []struct {
		name       string
		typeLine   string
		oracleText string
		faces      []cardFace
		want       bool
	}{
		{"legendary creature", "Legendary Creature — Goblin", "{T}: Do a thing.", nil, true},
		{"planeswalker granted by text", "Legendary Planeswalker — Test", "Test can be your commander.", nil, true},
		{"plain planeswalker", "Legendary Planeswalker — Test", "[+1]: Draw a card.", nil, false},
		{"non-legendary creature", "Creature — Rat", "Trample", nil, false},
		{"legendary but not creature", "Legendary Artifact", "Tap for mana.", nil, false},
		{"qualifying back face", "Legendary Enchantment", "", []cardFace{
			{TypeLine: "Legendary Enchantment", OracleText: ""},
			{TypeLine: "Legendary Creature — God", OracleText: ""},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveCanBeCommander(tc.typeLine, tc.oracleText, tc.faces); got != tc.want {
				t.Fatalf("deriveCanBeCommander(%q, %q) = %v, want %v", tc.typeLine, tc.oracleText, got, tc.want)
			}
		})
	}
}
