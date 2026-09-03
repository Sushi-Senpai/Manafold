// Package deckio parses and emits the community decklist text formats. It is a
// pure package: it turns raw text into structured lines and structured entries
// back into text, and performs no card-name resolution and no database access —
// name resolution against the mirror and the audited write of deck_cards live in
// internal/api (imports.go). Keeping it pure makes the tolerant grammar
// exhaustively table-testable.
//
// @spec PORT-002, PORT-003, PORT-005, PORT-007
package deckio

import "fmt"

// Format is a decklist text format Manafold reads. All four share one tolerant
// line grammar; they differ only in how sections are marked.
type Format string

const (
	FormatPlaintext Format = "plaintext"
	FormatMTGA      Format = "mtga"
	FormatMoxfield  Format = "moxfield"
	FormatArchidekt Format = "archidekt"
)

// ParseFormat validates a client-supplied source_format string.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatPlaintext, FormatMTGA, FormatMoxfield, FormatArchidekt:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown source format %q (want plaintext, mtga, moxfield, or archidekt)", s)
	}
}

// Boards used by the parser output. They mirror deck_cards.board.
const (
	BoardCommand   = "command"
	BoardMain      = "main"
	BoardMaybe     = "maybe"
	BoardSideboard = "sideboard"
)

// Line is one parsed decklist entry. Name is kept exactly as written (one face
// of a split/DFC card is fine — resolution folds it to the whole card); SetCode
// and CollectorNumber are captured when present but are advisory in M2 (printing
// selection is a later milestone). Category is populated only from an Archidekt
// inline [Category] tag.
type Line struct {
	Quantity        int    `json:"quantity"`
	Name            string `json:"name"`
	Board           string `json:"board"`
	Category        string `json:"category,omitempty"`
	SetCode         string `json:"set_code,omitempty"`
	CollectorNumber string `json:"collector_number,omitempty"`
	Raw             string `json:"raw"`
}

// ParseResult is the structured form of a decklist. Lines holds every entry the
// grammar recognised; Rejected holds non-blank, non-header lines the grammar
// could not parse at all (a malformed line — distinct from a line that parses
// but whose name later fails to resolve against the mirror, which is the API
// layer's "unresolved" list, PORT-004).
type ParseResult struct {
	Lines    []Line   `json:"lines"`
	Rejected []string `json:"rejected"`
}

// Entry is the input to the emitters: one resolved deck entry to write out.
type Entry struct {
	Quantity        int
	Name            string
	Board           string
	SetCode         string
	CollectorNumber string
}
