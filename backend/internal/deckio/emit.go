package deckio

import (
	"sort"
	"strconv"
	"strings"
)

// EmitPlaintext writes the universal plain-text decklist: a "Commander:" section
// (when the deck has one), then "Deck", then "Maybeboard" / "Sideboard" when
// non-empty. One "<qty> <name>" entry per line, entries sorted by name within a
// section. Set/collector references are not written — plain text is the
// tool-agnostic fallback.
//
// @spec PORT-007
func EmitPlaintext(entries []Entry) string {
	return emit(entries, false)
}

// EmitMTGA writes the MTG Arena import form: a "Commander" section, a blank line,
// "Deck", then a blank line and "Sideboard" when non-empty. Each entry carries
// its "(SET) collector#" when known, which is what Arena's importer expects.
//
// @spec PORT-007
func EmitMTGA(entries []Entry) string {
	return emit(entries, true)
}

func emit(entries []Entry, withPrinting bool) string {
	byBoard := map[string][]Entry{}
	for _, e := range entries {
		b := e.Board
		if b == "" {
			b = BoardMain
		}
		byBoard[b] = append(byBoard[b], e)
	}
	for _, list := range byBoard {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}

	var b strings.Builder
	section := func(header string, list []Entry) {
		if len(list) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(header)
		b.WriteString("\n")
		for _, e := range list {
			b.WriteString(formatEntry(e, withPrinting))
			b.WriteString("\n")
		}
	}

	section("Commander", byBoard[BoardCommand])
	section("Deck", byBoard[BoardMain])
	section("Maybeboard", byBoard[BoardMaybe])
	section("Sideboard", byBoard[BoardSideboard])
	return b.String()
}

func formatEntry(e Entry, withPrinting bool) string {
	qty := e.Quantity
	if qty <= 0 {
		qty = 1
	}
	line := strconv.Itoa(qty) + " " + e.Name
	if withPrinting && e.SetCode != "" && e.CollectorNumber != "" {
		line += " (" + strings.ToUpper(e.SetCode) + ") " + e.CollectorNumber
	}
	return line
}
