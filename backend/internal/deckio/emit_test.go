package deckio

import (
	"strings"
	"testing"
)

// @spec PORT-007
func TestEmitPlaintext_SectionsAndSorting(t *testing.T) {
	entries := []Entry{
		{Quantity: 1, Name: "Sol Ring", Board: BoardMain, SetCode: "LTC", CollectorNumber: "284"},
		{Quantity: 1, Name: "Arcane Signet", Board: BoardMain},
		{Quantity: 1, Name: "Atraxa, Praetors' Voice", Board: BoardCommand},
		{Quantity: 1, Name: "Cultivate", Board: BoardMaybe},
	}
	got := EmitPlaintext(entries)
	want := "Commander\n1 Atraxa, Praetors' Voice\n\nDeck\n1 Arcane Signet\n1 Sol Ring\n\nMaybeboard\n1 Cultivate\n"
	if got != want {
		t.Fatalf("plaintext emit mismatch:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "(LTC)") {
		t.Fatal("plain text must not carry set/collector references")
	}
}

// @spec PORT-007
func TestEmitMTGA_CarriesPrintingRefs(t *testing.T) {
	entries := []Entry{
		{Quantity: 4, Name: "Lightning Bolt", Board: BoardMain, SetCode: "2xm", CollectorNumber: "129"},
		{Quantity: 1, Name: "Rest in Peace", Board: BoardSideboard, SetCode: "2X2", CollectorNumber: "26"},
	}
	got := EmitMTGA(entries)
	want := "Deck\n4 Lightning Bolt (2XM) 129\n\nSideboard\n1 Rest in Peace (2X2) 26\n"
	if got != want {
		t.Fatalf("mtga emit mismatch:\n got %q\nwant %q", got, want)
	}
}

// Emit output round-trips back through Parse to the same logical entries.
// @spec PORT-002, PORT-007
func TestEmitParseRoundTrip(t *testing.T) {
	entries := []Entry{
		{Quantity: 1, Name: "Kenrith, the Returned King", Board: BoardCommand},
		{Quantity: 1, Name: "Sol Ring", Board: BoardMain},
		{Quantity: 10, Name: "Forest", Board: BoardMain},
	}
	round := Parse(FormatPlaintext, EmitPlaintext(entries))
	if len(round.Lines) != 3 || len(round.Rejected) != 0 {
		t.Fatalf("round trip changed shape: %#v", round)
	}
	byName := map[string]Line{}
	for _, l := range round.Lines {
		byName[l.Name] = l
	}
	if byName["Kenrith, the Returned King"].Board != BoardCommand {
		t.Fatalf("commander board lost in round trip: %#v", byName)
	}
	if byName["Forest"].Quantity != 10 {
		t.Fatalf("quantity lost in round trip: %#v", byName["Forest"])
	}
}
