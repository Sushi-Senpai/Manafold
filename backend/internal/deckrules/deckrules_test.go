package deckrules

import (
	"reflect"
	"testing"
)

func intp(n int) *int    { return &n }
func boolp(b bool) *bool { return &b }

func card(name string, identity ...string) CardFacts {
	return CardFacts{ID: name, Name: name, ColorIdentity: identity, LegalitiesCommander: "legal"}
}

func entry(c CardFacts, board string, qty int) Entry {
	return Entry{Card: c, Board: board, Quantity: qty}
}

// @spec DECK-004
func TestValidate_ColorIdentity(t *testing.T) {
	// Deck identity is Gruul (R,G). A blue card is outside it; a DFC whose
	// Scryfall color_identity is ["B","R"] is partly outside it.
	deckID := []string{"R", "G"}
	blue := card("Counterspell", "U")
	dfc := card("Valki, God of Lies", "B", "R") // modal DFC, identity union across faces
	inGruul := card("Cultivate", "G")

	report := Validate(ValidationInput{
		DeckColorIdentity: deckID,
		Commander:         &CardFacts{ID: "cmd", Name: "Radha", ColorIdentity: deckID, CanBeCommander: true},
		Entries: []Entry{
			entry(blue, "main", 1),
			entry(dfc, "main", 1),
			entry(inGruul, "main", 1),
		},
	})

	got := map[string][]string{}
	for _, v := range report.ColorIdentityViolations {
		got[v.CardName] = v.Offending
	}
	want := map[string][]string{
		"Counterspell":       {"U"},
		"Valki, God of Lies": {"B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("colour-identity violations\n got: %#v\nwant: %#v", got, want)
	}
}

// @spec DECK-021
func TestValidate_PartnerCombinedIdentity(t *testing.T) {
	// Thrasios (G,U) + Tymna (W,B) partner into a WUBG deck. A WUBG card is
	// legal; a red card is not.
	thrasios := CardFacts{
		ID: "thrasios", Name: "Thrasios, Triton Hero",
		ColorIdentity: []string{"G", "U"}, CanBeCommander: true,
		Keywords: []string{"Partner"},
	}
	tymna := CardFacts{
		ID: "tymna", Name: "Tymna the Weaver",
		ColorIdentity: []string{"W", "B"}, CanBeCommander: true,
		Keywords: []string{"Partner"},
	}

	deckID := ComputeDeckColorIdentity(&thrasios, &tymna)
	if !reflect.DeepEqual(deckID, []string{"W", "U", "B", "G"}) {
		t.Fatalf("combined identity = %#v, want [W U B G]", deckID)
	}

	report := Validate(ValidationInput{
		DeckColorIdentity: deckID,
		Commander:         &thrasios,
		Partner:           &tymna,
		Entries: []Entry{
			entry(card("Kess, Dissident Mage", "U", "B", "R"), "main", 1), // has red → outside
			entry(card("Sphinx's Revelation", "W", "U"), "main", 1),       // inside
		},
	})
	if len(report.CommanderIssues) != 0 {
		t.Fatalf("valid Partner pairing should raise no commander issues, got %v", report.CommanderIssues)
	}
	if len(report.ColorIdentityViolations) != 1 || report.ColorIdentityViolations[0].CardName != "Kess, Dissident Mage" {
		t.Fatalf("expected only Kess flagged, got %#v", report.ColorIdentityViolations)
	}
}

// @spec DECK-021
func TestValidate_IncompatiblePartner(t *testing.T) {
	// A plain-Partner card and a "Partner with" card that names someone else
	// are not a valid pairing.
	plain := CardFacts{ID: "a", Name: "Thrasios, Triton Hero", CanBeCommander: true, Keywords: []string{"Partner"}}
	restricted := CardFacts{
		ID: "b", Name: "Will Kenrith", CanBeCommander: true,
		OracleText: "Partner with Rowan Kenrith (When this creature enters, target opponent may put Rowan Kenrith into your hand from your library, then shuffle.)",
	}
	report := Validate(ValidationInput{
		DeckColorIdentity: []string{"U"},
		Commander:         &plain,
		Partner:           &restricted,
	})
	found := false
	for _, issue := range report.CommanderIssues {
		if issue == "Thrasios, Triton Hero and Will Kenrith are not a valid partner pairing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an incompatible-partner issue, got %v", report.CommanderIssues)
	}
}

// @spec DECK-006
func TestValidate_Singleton(t *testing.T) {
	tests := []struct {
		name      string
		card      CardFacts
		qty       int
		wantViol  bool
		wantLimit int
	}{
		{
			name: "normal card over 1 is a violation",
			card: CardFacts{ID: "sol", Name: "Sol Ring", LegalitiesCommander: "legal"},
			qty:  2, wantViol: true, wantLimit: 1,
		},
		{
			name: "basic land at any quantity is fine",
			card: CardFacts{ID: "island", Name: "Island", IsBasicLand: true, LegalitiesCommander: "legal"},
			qty:  37, wantViol: false,
		},
		{
			name: "singleton_limit 0 (any number) is fine",
			card: CardFacts{ID: "dragons", Name: "Dragon's Approach", SingletonLimit: intp(0), LegalitiesCommander: "legal"},
			qty:  19, wantViol: false,
		},
		{
			name: "singleton_limit 7 caps at 7 — 8 is a violation",
			card: CardFacts{ID: "dwarves", Name: "Seven Dwarves", SingletonLimit: intp(7), LegalitiesCommander: "legal"},
			qty:  8, wantViol: true, wantLimit: 7,
		},
		{
			name: "singleton_limit 7 — exactly 7 is fine",
			card: CardFacts{ID: "dwarves", Name: "Seven Dwarves", SingletonLimit: intp(7), LegalitiesCommander: "legal"},
			qty:  7, wantViol: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := Validate(ValidationInput{
				DeckColorIdentity: []string{},
				Commander:         &CardFacts{ID: "c", Name: "C", CanBeCommander: true},
				Entries:           []Entry{entry(tc.card, "main", tc.qty)},
			})
			if got := len(report.SingletonViolations) == 1; got != tc.wantViol {
				t.Fatalf("singleton violation present = %v, want %v (report %#v)", got, tc.wantViol, report.SingletonViolations)
			}
			if tc.wantViol && report.SingletonViolations[0].Limit != tc.wantLimit {
				t.Fatalf("reported limit = %d, want %d", report.SingletonViolations[0].Limit, tc.wantLimit)
			}
		})
	}
}

// @spec DECK-008
func TestValidate_Banlist(t *testing.T) {
	bannedByScryfall := CardFacts{ID: "crypt", Name: "Mana Crypt", LegalitiesCommander: "banned"}
	unbannedByOverride := CardFacts{ID: "crypt2", Name: "Mana Crypt", LegalitiesCommander: "banned", OverrideBanned: boolp(false)}
	bannedByOverride := CardFacts{ID: "future", Name: "Freshly Banned Card", LegalitiesCommander: "legal", OverrideBanned: boolp(true)}
	legal := CardFacts{ID: "ring", Name: "Sol Ring", LegalitiesCommander: "legal"}

	report := Validate(ValidationInput{
		DeckColorIdentity: []string{},
		Commander:         &CardFacts{ID: "c", Name: "C", CanBeCommander: true},
		Entries: []Entry{
			entry(bannedByScryfall, "main", 1),
			entry(unbannedByOverride, "main", 1),
			entry(bannedByOverride, "main", 1),
			entry(legal, "main", 1),
		},
	})

	got := map[string]string{}
	for _, v := range report.BanlistViolations {
		got[v.CardName] = v.Reason
	}
	want := map[string]string{
		"Mana Crypt":          "banned in Commander",
		"Freshly Banned Card": "banned by manual override",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("banlist violations\n got: %#v\nwant: %#v", got, want)
	}
}

// @spec DECK-008
func TestValidate_Count(t *testing.T) {
	var entries []Entry
	// 98 distinct main cards + a synthesized commander on the command board = 99.
	for i := 0; i < 98; i++ {
		c := CardFacts{ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Name: "filler", LegalitiesCommander: "legal"}
		entries = append(entries, entry(c, "main", 1))
	}
	cmd := CardFacts{ID: "cmd", Name: "Commander", CanBeCommander: true}
	entries = append(entries, entry(cmd, "command", 1))
	// A maybeboard card must not count.
	entries = append(entries, entry(CardFacts{ID: "mb", Name: "Maybe", LegalitiesCommander: "legal"}, "maybe", 5))

	report := Validate(ValidationInput{
		DeckColorIdentity: []string{},
		Commander:         &cmd,
		Entries:           entries,
	})
	if report.MainCommandCount != 99 {
		t.Fatalf("MainCommandCount = %d, want 99", report.MainCommandCount)
	}
	if report.CountDeviation != -1 {
		t.Fatalf("CountDeviation = %d, want -1", report.CountDeviation)
	}
	if report.Legal {
		t.Fatalf("a 99-card deck must not be Legal")
	}
}

// @spec DECK-020
func TestValidate_CommanderShape(t *testing.T) {
	// No commander assigned.
	r1 := Validate(ValidationInput{DeckColorIdentity: []string{}})
	if len(r1.CommanderIssues) == 0 {
		t.Fatalf("expected a 'no commander' issue")
	}

	// Assigned commander that cannot be a commander.
	r2 := Validate(ValidationInput{
		DeckColorIdentity: []string{},
		Commander:         &CardFacts{ID: "x", Name: "Lightning Bolt", CanBeCommander: false},
	})
	found := false
	for _, i := range r2.CommanderIssues {
		if i == "Lightning Bolt cannot be a commander" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'cannot be a commander' issue, got %v", r2.CommanderIssues)
	}
}

// @spec DECK-008
func TestValidate_LegalDeckIsLegal(t *testing.T) {
	cmd := CardFacts{ID: "cmd", Name: "Krenko, Mob Boss", ColorIdentity: []string{"R"}, CanBeCommander: true, LegalitiesCommander: "legal"}
	var entries []Entry
	entries = append(entries, entry(cmd, "command", 1))
	for i := 0; i < 63; i++ {
		entries = append(entries, entry(CardFacts{
			ID:   "spell" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
			Name: "Goblin", ColorIdentity: []string{"R"}, LegalitiesCommander: "legal",
		}, "main", 1))
	}
	entries = append(entries, entry(CardFacts{ID: "mtn", Name: "Mountain", IsBasicLand: true, LegalitiesCommander: "legal"}, "main", 36))

	report := Validate(ValidationInput{
		DeckColorIdentity: []string{"R"},
		Commander:         &cmd,
		Entries:           entries,
	})
	if !report.Legal {
		t.Fatalf("expected a legal 100-card mono-red deck, got %#v", report)
	}
	if report.MainCommandCount != 100 {
		t.Fatalf("MainCommandCount = %d, want 100", report.MainCommandCount)
	}
}
