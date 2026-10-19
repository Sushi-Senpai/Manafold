package deckroles

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// seedCards loads the real Scryfall cards in backend/seed/cards.json, keyed by
// name, so the classifier is tested on genuine Oracle text.
func seedCards(t *testing.T) map[string]Card {
	t.Helper()
	b, err := os.ReadFile("../../seed/cards.json")
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	var raw []struct {
		Name       string   `json:"name"`
		TypeLine   string   `json:"type_line"`
		OracleText string   `json:"oracle_text"`
		Keywords   []string `json:"keywords"`
		CardFaces  []Face   `json:"card_faces"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("decode seed: %v", err)
	}
	out := make(map[string]Card, len(raw))
	for _, c := range raw {
		out[c.Name] = Card{TypeLine: c.TypeLine, OracleText: c.OracleText, Keywords: c.Keywords, Faces: c.CardFaces}
	}
	return out
}

// @spec DECK-107
func TestOracleClassifier_SeedCards(t *testing.T) {
	cards := seedCards(t)
	cases := map[string][]Role{
		// Ramp: mana rocks and dorks, land search to the battlefield, land from
		// hand, Treasure makers, a battle whose front face fetches lands.
		"Sol Ring":                 {Ramp},
		"Arcane Signet":            {Ramp},
		"Llanowar Elves":           {Ramp},
		"Birds of Paradise":        {Ramp},
		"Cultivate":                {Ramp},
		"Kodama's Reach":           {Ramp},
		"Smothering Tithe":         {Ramp}, // an opponent drawing is not card draw
		"Solemn Simulacrum":        {Ramp, CardDraw},
		"Chulane, Teller of Tales": {Ramp, CardDraw},
		"Invasion of Zendikar // Awakened Skyclave": {Ramp},
		// Card draw.
		"Rhystic Study": {CardDraw},
		"Skullclamp":    {CardDraw},
		"The Ur-Dragon": {CardDraw},
		// Targeted removal; Path's "search their library" is not our ramp.
		"Swords to Plowshares":       {TargetedRemoval},
		"Path to Exile":              {TargetedRemoval},
		"Beast Within":               {TargetedRemoval},
		"Chandra, Torch of Defiance": {Ramp, TargetedRemoval},
		// Overload makes targeted bounce a board wipe too.
		"Cyclonic Rift": {TargetedRemoval, BoardWipe},
		// Board wipes.
		"Wrath of God":    {BoardWipe},
		"Blasphemous Act": {BoardWipe},
		"Farewell":        {BoardWipe},
		// None of the roles.
		"Counterspell":                    nil,
		"Demonic Tutor":                   nil,
		"Vampiric Tutor":                  nil,
		"Fabricate":                       nil,
		"Eternal Witness":                 nil, // returns a card from the graveyard, not removal
		"Kenrith, the Returned King":      nil, // "target player draws" is not counted
		"Lightning Greaves":               nil,
		"Propaganda":                      nil,
		"Malakir Rebirth // Malakir Mire": nil, // front face only; the land back is not ramp
		"Command Tower":                   nil, // lands are never classified
		"Forest":                          nil,
		"Brightclimb Pathway // Grimclimb Pathway": nil,
	}
	for name, want := range cases {
		c, ok := cards[name]
		if !ok {
			t.Fatalf("seed card %q missing", name)
		}
		if got := (OracleClassifier{}).Classify(c); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: roles = %v, want %v", name, got, want)
		}
	}
}

// @spec DECK-107
func TestOracleClassifier_Rules(t *testing.T) {
	cases := []struct {
		name string
		card Card
		want []Role
	}{
		{"burn any target", Card{TypeLine: "Instant", OracleText: "Lightning Bolt deals 3 damage to any target."}, []Role{TargetedRemoval}},
		{"destroy target nonblack creature", Card{TypeLine: "Instant", OracleText: "Destroy target nonblack creature."}, []Role{TargetedRemoval}},
		{"shrink", Card{TypeLine: "Instant", OracleText: "Target creature gets -3/-3 until end of turn."}, []Role{TargetedRemoval}},
		{"cantrip removal", Card{TypeLine: "Instant", OracleText: "Destroy target artifact or enchantment. Draw a card."}, []Role{CardDraw, TargetedRemoval}},
		{"graveyard hate is not removal", Card{TypeLine: "Instant", OracleText: "Exile target card from a graveyard."}, nil},
		{"own-creature blink is not removal", Card{TypeLine: "Instant", OracleText: "Exile target creature you control, then return it to the battlefield under its owner's control."}, nil},
		{"mass shrink", Card{TypeLine: "Sorcery", OracleText: "As an additional cost to cast this spell, pay X life.\nAll creatures get -X/-X until end of turn."}, []Role{BoardWipe}},
		{"mass bounce", Card{TypeLine: "Instant", OracleText: "Return all creatures to their owners' hands."}, []Role{BoardWipe}},
		{"graveyard wipe is not a board wipe", Card{TypeLine: "Instant", OracleText: "Exile all cards from target player's graveyard."}, nil},
		{"ritual is not ramp", Card{TypeLine: "Instant", OracleText: "Add {B}{B}{B}."}, nil},
		{"extra land drop", Card{TypeLine: "Enchantment", OracleText: "You may play an additional land on each of your turns."}, []Role{Ramp}},
		{"draw trigger is not draw", Card{TypeLine: "Enchantment", OracleText: "Whenever you draw a card, you gain 1 life."}, nil},
		{"would-draw clause skipped, instead-draw counts", Card{TypeLine: "Creature — Human", OracleText: "If an opponent would draw a card except the first one they draw in each of their draw steps, instead you draw a card."}, []Role{CardDraw}},
		{"reminder text is ignored", Card{TypeLine: "Creature — Human", OracleText: "Investigate. (Create a Clue token. It's an artifact with \"{2}, Sacrifice this artifact: Draw a card.\")"}, nil},
		{"land creature is a land", Card{TypeLine: "Land Creature — Forest Dryad", OracleText: "({T}: Add {G}.)"}, nil},
	}
	for _, tc := range cases {
		if got := (OracleClassifier{}).Classify(tc.card); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: roles = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type stubClassifier map[string][]Role

func (s stubClassifier) Classify(c Card) []Role { return s[c.OracleText] }

// @spec DECK-106
func TestCount_QuantityAndMultipleRoles(t *testing.T) {
	cl := stubClassifier{
		"rock":    {Ramp},
		"cantrip": {CardDraw, TargetedRemoval},
		"nothing": nil,
	}
	got := Count(cl, []Entry{
		{Card: Card{OracleText: "rock"}, Quantity: 3},
		{Card: Card{OracleText: "cantrip"}, Quantity: 1},
		{Card: Card{OracleText: "nothing"}, Quantity: 5},
	})
	want := map[Role]int{Ramp: 3, CardDraw: 1, TargetedRemoval: 1, BoardWipe: 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Count = %v, want %v", got, want)
	}
}
