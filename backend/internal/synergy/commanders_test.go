package synergy

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// @spec SYN-001
func TestParseCommanderList_IgnoresBlankLinesAndComments(t *testing.T) {
	in := "# popular\n\nKrenko, Mob Boss\n  Edgar Markov  \n#Atraxa\n"
	got, err := ParseCommanderList(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Krenko, Mob Boss", "Edgar Markov"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// @spec SYN-001
func TestLoadCommanders_EmbeddedDefaultAndFileOverride(t *testing.T) {
	def, err := LoadCommanders("")
	if err != nil {
		t.Fatal(err)
	}
	if len(def) < 5 {
		t.Fatalf("embedded default list has %d commanders, want a modest list", len(def))
	}
	path := filepath.Join(t.TempDir(), "list.txt")
	if err := os.WriteFile(path, []byte("Meren of Clan Nel Toth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCommanders(path)
	if err != nil || !reflect.DeepEqual(got, []string{"Meren of Clan Nel Toth"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := LoadCommanders(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("expected an error for a missing commander file")
	}
}
