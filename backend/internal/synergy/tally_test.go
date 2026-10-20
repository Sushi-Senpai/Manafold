package synergy

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

const (
	krenko        = "aaaaaaaa-0000-4000-8000-000000000001"
	solRing       = "aaaaaaaa-0000-4000-8000-000000000002"
	warchief      = "aaaaaaaa-0000-4000-8000-000000000003"
	skullclamp    = "aaaaaaaa-0000-4000-8000-000000000004"
	chieftain     = "aaaaaaaa-0000-4000-8000-000000000008"
	impactTremors = "aaaaaaaa-0000-4000-8000-000000000009"
)

func loadDeck(t *testing.T, name string) deckDetail {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var d deckDetail
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return d
}

// @spec SYN-012
func TestTallyDeck_ExcludesMaybeboardCommanderDeletedAndBasics(t *testing.T) {
	// deck_101 has the commander (tagged), Sol Ring, Warchief x2, 30 Mountains,
	// a Maybeboard card, a deleted card, and Goblin Chieftain.
	got := tallyDeck(loadDeck(t, "deck_101.json"), krenko)
	want := []string{solRing, warchief, chieftain}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tally = %v, want %v", got, want)
	}
}

// @spec SYN-012
func TestTallyDeck_ExcludesUntaggedCommanderByOracleID(t *testing.T) {
	// deck_106 files Krenko under Creature, not Commander.
	got := tallyDeck(loadDeck(t, "deck_106.json"), krenko)
	want := []string{solRing, warchief, impactTremors}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tally = %v, want %v", got, want)
	}
}

// @spec SYN-012
func TestTallyDeck_CountsQuantityOnce(t *testing.T) {
	d := loadDeck(t, "deck_101.json")
	d.Cards = append(d.Cards, d.Cards[2]) // Goblin Warchief listed again
	got := tallyDeck(d, krenko)
	n := 0
	for _, id := range got {
		if id == warchief {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Goblin Warchief tallied %d times, want 1", n)
	}
}

// @spec SYN-013
func TestSample_DeduplicatesIdenticalDecklists(t *testing.T) {
	s := newSample()
	// deck_101 and deck_107 differ only in excluded entries (basics, maybeboard).
	s.add(tallyDeck(loadDeck(t, "deck_101.json"), krenko))
	s.add(tallyDeck(loadDeck(t, "deck_107.json"), krenko))
	s.add(tallyDeck(loadDeck(t, "deck_106.json"), krenko))
	s.add(nil) // empty decks are neither counted nor duplicates

	if s.decks != 2 || s.duplicates != 1 {
		t.Fatalf("decks=%d duplicates=%d, want 2 and 1", s.decks, s.duplicates)
	}
	if got := s.cardDecks[solRing]; got != 2 {
		t.Errorf("Sol Ring in %d decks, want 2", got)
	}
	if got := s.cardDecks[chieftain]; got != 1 {
		t.Errorf("Goblin Chieftain in %d decks, want 1", got)
	}
	if got := s.inclusionRate(chieftain); got != 0.5 {
		t.Errorf("Goblin Chieftain inclusion = %v, want 0.5", got)
	}
}

func TestCanonicalIdentityAndBack(t *testing.T) {
	cases := map[string][]string{
		"C":     {},
		"R":     {"R"},
		"WUBRG": {"G", "R", "B", "U", "W"},
		"UR":    {"R", "U"},
	}
	for want, in := range cases {
		if got := CanonicalIdentity(in); got != want {
			t.Errorf("CanonicalIdentity(%v) = %q, want %q", in, got, want)
		}
		if got := CanonicalIdentity(identityArray(want)); got != want {
			t.Errorf("round trip of %q = %q", want, got)
		}
	}
}

func TestFrontFace(t *testing.T) {
	if got := frontFace("Esika, God of the Tree // The Prismatic Bridge"); got != "Esika, God of the Tree" {
		t.Errorf("frontFace = %q", got)
	}
	if got := frontFace("Krenko, Mob Boss"); got != "Krenko, Mob Boss" {
		t.Errorf("frontFace = %q", got)
	}
}
