package deckio

import (
	"regexp"
	"strconv"
	"strings"
)

// entryRe matches "<qty> <rest>" or "<qty>x <rest>" at the head of a line. The
// rest (name, optional set/collector, foil marks, tags) is picked apart by
// stripTrailers.
var entryRe = regexp.MustCompile(`^(\d+)\s*[xX]?\s+(.+)$`)

// setCollectorRe matches a trailing Scryfall-style "(SET) 123" or "(SET) 123a"
// printing reference.
var setCollectorRe = regexp.MustCompile(`\s*\(([A-Za-z0-9]{2,6})\)\s+([A-Za-z0-9]+[A-Za-z]?)\s*$`)

// categoryRe matches a trailing Archidekt "[Category]" or "[Category{top}]" tag.
var categoryRe = regexp.MustCompile(`\s*\[([^\]]+?)\]\s*$`)

// foilRe matches Moxfield/other trailing foil markers: "*F*", "*E*", "^Foil^".
var foilRe = regexp.MustCompile(`\s*(\*[A-Za-z]\*|\^[A-Za-z]+\^)\s*$`)

// Parse turns raw decklist text of the given format into a ParseResult. It never
// returns an error for malformed content — an unparseable line goes into
// Rejected — so the caller can always show the user what was and was not
// understood.
//
// @spec PORT-002, PORT-003
func Parse(format Format, text string) ParseResult {
	var res ParseResult
	board := BoardMain
	// For MTGA/Moxfield paste with no explicit section headers, the first blank
	// line after at least one entry separates the mainboard from the sideboard.
	blankSeparates := format == FormatMTGA || format == FormatMoxfield
	sawEntry := false
	sawExplicitSection := false

	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(rawLine, "\ufeff"))
		if line == "" {
			if blankSeparates && sawEntry && !sawExplicitSection && board == BoardMain {
				board = BoardSideboard
			}
			continue
		}
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue // comment line
		}

		// MTGO-style "SB: 1 Card" sideboard prefix.
		if rest, ok := strings.CutPrefix(line, "SB:"); ok {
			line = strings.TrimSpace(rest)
			if l, parsed := parseEntry(line, BoardSideboard); parsed {
				res.Lines = append(res.Lines, l)
				sawEntry = true
			} else {
				res.Rejected = append(res.Rejected, line)
			}
			continue
		}

		if b, ok := sectionHeader(line); ok {
			board = b
			sawExplicitSection = true
			continue
		}

		if l, parsed := parseEntry(line, board); parsed {
			res.Lines = append(res.Lines, l)
			sawEntry = true
			continue
		}
		res.Rejected = append(res.Rejected, line)
	}
	return res
}

// sectionHeader recognises a board header, tolerating a trailing "(N)" count
// (Archidekt) and an optional trailing colon (plain text). It only matches a
// line that is *nothing but* a header keyword so a card literally named
// "Commander" (there is none, but the rule must be principled) is never eaten.
func sectionHeader(line string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(line))
	s = strings.TrimSuffix(s, ":")
	// strip a trailing "(12)" count
	if i := strings.LastIndex(s, "("); i > 0 && strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[:i])
	}
	switch s {
	case "commander", "commanders", "command", "command zone":
		return BoardCommand, true
	case "companion":
		// MTGA places the companion at the top of the sideboard.
		return BoardSideboard, true
	case "deck", "mainboard", "main", "maindeck":
		return BoardMain, true
	case "sideboard", "side", "sb":
		return BoardSideboard, true
	case "maybeboard", "maybe", "considering", "maybes":
		return BoardMaybe, true
	default:
		return "", false
	}
}

// parseEntry parses one "<qty> <name> …" line onto the given board.
func parseEntry(line, board string) (Line, bool) {
	m := entryRe.FindStringSubmatch(line)
	if m == nil {
		return Line{}, false
	}
	qty, err := strconv.Atoi(m[1])
	if err != nil || qty <= 0 {
		return Line{}, false
	}
	name, setCode, collector, category := stripTrailers(m[2])
	name = strings.TrimSpace(name)
	if name == "" {
		return Line{}, false
	}
	return Line{
		Quantity:        qty,
		Name:            name,
		Board:           board,
		Category:        category,
		SetCode:         strings.ToUpper(setCode),
		CollectorNumber: collector,
		Raw:             line,
	}, true
}

// stripTrailers peels an Archidekt [Category] tag, a foil marker, and a
// "(SET) collector#" reference off the end of a card name, in the order they
// conventionally appear, and returns the bare name plus whatever was found.
func stripTrailers(s string) (name, setCode, collector, category string) {
	name = strings.TrimSpace(s)

	if m := categoryRe.FindStringSubmatch(name); m != nil {
		cat := strings.TrimSpace(m[1])
		// Archidekt writes "[Ramp{top}]" / "[Ramp{noDeck}]" — keep just the name.
		if i := strings.Index(cat, "{"); i >= 0 {
			cat = strings.TrimSpace(cat[:i])
		}
		category = cat
		name = strings.TrimSpace(name[:len(name)-len(m[0])])
	}

	if m := foilRe.FindStringSubmatch(name); m != nil {
		name = strings.TrimSpace(name[:len(name)-len(m[0])])
	}

	if m := setCollectorRe.FindStringSubmatch(name); m != nil {
		setCode = m[1]
		collector = m[2]
		name = strings.TrimSpace(name[:len(name)-len(m[0])])
	}

	return name, setCode, collector, category
}
