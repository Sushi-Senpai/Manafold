package deckio

import (
	"reflect"
	"testing"
)

// @spec PORT-002
func TestParse_PlaintextSectionsAndBoards(t *testing.T) {
	in := "Commander:\n1 Atraxa, Praetors' Voice\n\nDeck\n1 Sol Ring\n1 Arcane Signet\n\nMaybeboard\n1 Cultivate\n\nSideboard\n1 Swords to Plowshares\n"
	got := Parse(FormatPlaintext, in)
	if len(got.Rejected) != 0 {
		t.Fatalf("unexpected rejected lines: %v", got.Rejected)
	}
	want := []Line{
		{Quantity: 1, Name: "Atraxa, Praetors' Voice", Board: BoardCommand, Raw: "1 Atraxa, Praetors' Voice"},
		{Quantity: 1, Name: "Sol Ring", Board: BoardMain, Raw: "1 Sol Ring"},
		{Quantity: 1, Name: "Arcane Signet", Board: BoardMain, Raw: "1 Arcane Signet"},
		{Quantity: 1, Name: "Cultivate", Board: BoardMaybe, Raw: "1 Cultivate"},
		{Quantity: 1, Name: "Swords to Plowshares", Board: BoardSideboard, Raw: "1 Swords to Plowshares"},
	}
	if !reflect.DeepEqual(got.Lines, want) {
		t.Fatalf("lines mismatch:\n got %#v\nwant %#v", got.Lines, want)
	}
}

// @spec PORT-002
func TestParse_MTGASetCodesAndBlankLineSideboard(t *testing.T) {
	in := "4 Lightning Bolt (2XM) 129\n1 Command Tower (ELD) 333\n\n2 Pyroblast (EMA) 130\n"
	got := Parse(FormatMTGA, in)
	if len(got.Rejected) != 0 {
		t.Fatalf("unexpected rejected: %v", got.Rejected)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %#v", len(got.Lines), got.Lines)
	}
	if got.Lines[0] != (Line{Quantity: 4, Name: "Lightning Bolt", Board: BoardMain, SetCode: "2XM", CollectorNumber: "129", Raw: "4 Lightning Bolt (2XM) 129"}) {
		t.Fatalf("line 0 = %#v", got.Lines[0])
	}
	if got.Lines[2].Board != BoardSideboard {
		t.Fatalf("line after blank separator should be sideboard, got %q", got.Lines[2].Board)
	}
}

// @spec PORT-003
func TestParse_ArchidektInlineCategoryTags(t *testing.T) {
	in := "Commander (1)\n1 Kenrith, the Returned King\n\nDeck (2)\n1 Cultivate [Ramp]\n1 Wrath of God [Board Wipe{top}]\n"
	got := Parse(FormatArchidekt, in)
	if len(got.Rejected) != 0 {
		t.Fatalf("unexpected rejected: %v", got.Rejected)
	}
	want := []Line{
		{Quantity: 1, Name: "Kenrith, the Returned King", Board: BoardCommand, Raw: "1 Kenrith, the Returned King"},
		{Quantity: 1, Name: "Cultivate", Board: BoardMain, Category: "Ramp", Raw: "1 Cultivate [Ramp]"},
		{Quantity: 1, Name: "Wrath of God", Board: BoardMain, Category: "Board Wipe", Raw: "1 Wrath of God [Board Wipe{top}]"},
	}
	if !reflect.DeepEqual(got.Lines, want) {
		t.Fatalf("lines mismatch:\n got %#v\nwant %#v", got.Lines, want)
	}
}

// @spec PORT-002
func TestParse_MoxfieldPasteWithFoilAndSideboardMarker(t *testing.T) {
	in := "1 Sol Ring (LTC) 284 *F*\n1 Smothering Tithe (RNA) 22\nSB: 1 Rest in Peace (2X2) 26\n"
	got := Parse(FormatMoxfield, in)
	if len(got.Rejected) != 0 {
		t.Fatalf("unexpected rejected: %v", got.Rejected)
	}
	if got.Lines[0].Name != "Sol Ring" || got.Lines[0].SetCode != "LTC" || got.Lines[0].CollectorNumber != "284" {
		t.Fatalf("foil trailer not stripped cleanly: %#v", got.Lines[0])
	}
	if got.Lines[2].Board != BoardSideboard || got.Lines[2].Name != "Rest in Peace" {
		t.Fatalf("SB: prefix not honoured: %#v", got.Lines[2])
	}
}

func TestParse_QuantityFormsAndRejects(t *testing.T) {
	in := "3x Forest\nnonsense line with no quantity\n1 Island\n0 Plains\n"
	got := Parse(FormatPlaintext, in)
	if len(got.Lines) != 2 {
		t.Fatalf("want 2 accepted lines (3x Forest, 1 Island), got %#v", got.Lines)
	}
	if got.Lines[0].Quantity != 3 || got.Lines[0].Name != "Forest" {
		t.Fatalf("3x form not parsed: %#v", got.Lines[0])
	}
	if len(got.Rejected) != 2 {
		t.Fatalf("want 2 rejected (no-quantity line, 0 Plains), got %v", got.Rejected)
	}
}

// A split-card face name is parsed verbatim; folding it to the whole card is the
// resolver's job (PORT-005), tested in the api layer.
func TestParse_SplitFaceNameKeptVerbatim(t *testing.T) {
	got := Parse(FormatPlaintext, "1 Fire\n")
	if got.Lines[0].Name != "Fire" {
		t.Fatalf("name = %q, want %q", got.Lines[0].Name, "Fire")
	}
}

func TestParseFormat(t *testing.T) {
	for _, ok := range []string{"plaintext", "mtga", "moxfield", "archidekt"} {
		if _, err := ParseFormat(ok); err != nil {
			t.Errorf("ParseFormat(%q) errored: %v", ok, err)
		}
	}
	if _, err := ParseFormat("cod"); err == nil {
		t.Error("ParseFormat(\"cod\") should error — out of scope")
	}
}
