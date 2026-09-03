// Package deckrules is the pure Commander-legality validator: no database
// access, no AI. It takes a deck and its cards' Oracle facts and returns a
// structured report. Keeping it pure makes it exhaustively table-testable and
// lets ai-assist reuse it to gate model output without a database round trip.
//
// @spec DECK-004, DECK-006, DECK-008, DECK-020, DECK-021
package deckrules

import "sort"

// wubrgOrder ranks colour letters so unions render in the conventional order.
var wubrgOrder = map[string]int{"W": 0, "U": 1, "B": 2, "R": 3, "G": 4}

// CardFacts is everything the validator needs to know about one card. All of it
// comes from card-data (color_identity verbatim from Scryfall; singleton_limit
// and can_be_commander derived at sync) plus a per-card banlist override.
type CardFacts struct {
	ID                     string
	Name                   string
	ColorIdentity          []string // WUBRG letters, verbatim from Scryfall
	CommanderColorIdentity []string // may be nil → falls back to ColorIdentity
	CanBeCommander         bool
	Keywords               []string
	OracleText             string
	TypeLine               string
	SingletonLimit         *int // nil = normal singleton, 0 = unlimited, N = capped at N
	IsBasicLand            bool
	LegalitiesCommander    string // "legal" | "banned" | "restricted" | "not_legal" | ""
	OverrideBanned         *bool  // banlist_overrides row: nil = none, true = ban, false = un-ban
}

// Entry is one card slotted onto a board with a quantity.
type Entry struct {
	Card     CardFacts
	Board    string // "command" | "main" | "maybe" | "sideboard"
	Quantity int
}

// ValidationInput is assembled by the deck handler: the deck's denormalised
// colour identity, its commander(s), and every card entry (including synthesized
// command-zone entries for the commander(s)).
type ValidationInput struct {
	DeckColorIdentity []string
	Commander         *CardFacts
	Partner           *CardFacts
	Entries           []Entry
}

type ColorIdentityViolation struct {
	CardID    string   `json:"card_id"`
	CardName  string   `json:"card_name"`
	Offending []string `json:"offending"` // colours outside the deck identity
}

type SingletonViolation struct {
	CardID   string `json:"card_id"`
	CardName string `json:"card_name"`
	Quantity int    `json:"quantity"`
	Limit    int    `json:"limit"` // the maximum allowed (1 for a normal singleton card)
}

type BanlistViolation struct {
	CardID   string `json:"card_id"`
	CardName string `json:"card_name"`
	Reason   string `json:"reason"`
}

type ValidationReport struct {
	ColorIdentityViolations []ColorIdentityViolation `json:"color_identity_violations"`
	SingletonViolations     []SingletonViolation     `json:"singleton_violations"`
	MainCommandCount        int                      `json:"main_command_count"`
	CountDeviation          int                      `json:"count_deviation"` // MainCommandCount - 100
	BanlistViolations       []BanlistViolation       `json:"banlist_violations"`
	CommanderIssues         []string                 `json:"commander_issues"`
	Legal                   bool                     `json:"legal"`
}

// countedBoards are the boards that count toward the 100-card deck and are
// checked for colour identity, singleton, and legality. maybe/sideboard are
// staging areas and are not validated.
func countedBoard(board string) bool { return board == "main" || board == "command" }

// Validate runs every Commander legality rule and returns a full report.
func Validate(in ValidationInput) ValidationReport {
	var r ValidationReport
	r.ColorIdentityViolations = []ColorIdentityViolation{}
	r.SingletonViolations = []SingletonViolation{}
	r.BanlistViolations = []BanlistViolation{}
	r.CommanderIssues = []string{}

	deckSet := toSet(in.DeckColorIdentity)

	// Aggregate quantities per card across the counted boards, keeping one
	// representative CardFacts per id and first-seen order.
	type agg struct {
		card  CardFacts
		total int
	}
	byCard := map[string]*agg{}
	var order []string
	for _, e := range in.Entries {
		if !countedBoard(e.Board) {
			continue
		}
		r.MainCommandCount += e.Quantity
		a, ok := byCard[e.Card.ID]
		if !ok {
			a = &agg{card: e.Card}
			byCard[e.Card.ID] = a
			order = append(order, e.Card.ID)
		}
		a.total += e.Quantity
	}
	r.CountDeviation = r.MainCommandCount - 100

	for _, id := range order {
		a := byCard[id]

		// Rule 1 — colour identity: every counted card's identity must be a
		// subset of the deck's identity. Offenders are reported, not rejected.
		if offending := outside(a.card.ColorIdentity, deckSet); len(offending) > 0 {
			r.ColorIdentityViolations = append(r.ColorIdentityViolations, ColorIdentityViolation{
				CardID: a.card.ID, CardName: a.card.Name, Offending: offending,
			})
		}

		// Rule 2 — singleton.
		if limit, unlimited := singletonLimit(a.card); !unlimited && a.total > limit {
			r.SingletonViolations = append(r.SingletonViolations, SingletonViolation{
				CardID: a.card.ID, CardName: a.card.Name, Quantity: a.total, Limit: limit,
			})
		}

		// Rule 4 — banlist. An override wins over Scryfall's field in both
		// directions.
		if banned, reason := banStatus(a.card); banned {
			r.BanlistViolations = append(r.BanlistViolations, BanlistViolation{
				CardID: a.card.ID, CardName: a.card.Name, Reason: reason,
			})
		}
	}

	// Rule 5 — commander shape.
	r.CommanderIssues = commanderIssues(in.Commander, in.Partner)

	r.Legal = len(r.ColorIdentityViolations) == 0 &&
		len(r.SingletonViolations) == 0 &&
		len(r.BanlistViolations) == 0 &&
		len(r.CommanderIssues) == 0 &&
		r.CountDeviation == 0

	return r
}

// singletonLimit returns the maximum allowed quantity for a card on the counted
// boards. unlimited is true when the card imposes no cap (a basic land or a
// singleton_limit of 0).
func singletonLimit(c CardFacts) (limit int, unlimited bool) {
	if c.IsBasicLand {
		return 0, true
	}
	if c.SingletonLimit == nil {
		return 1, false
	}
	if *c.SingletonLimit == 0 {
		return 0, true
	}
	return *c.SingletonLimit, false
}

func banStatus(c CardFacts) (banned bool, reason string) {
	if c.OverrideBanned != nil {
		if *c.OverrideBanned {
			return true, "banned by manual override"
		}
		return false, ""
	}
	if c.LegalitiesCommander == "banned" {
		return true, "banned in Commander"
	}
	return false, ""
}

// ComputeDeckColorIdentity returns the union of a deck's commander(s)' commander
// colour identities, in WUBRG order. A card's CommanderColorIdentity is used
// when set, otherwise its ColorIdentity. Used by the deck handler to refresh
// decks.color_identity whenever a commander is assigned or cleared (DECK-003).
func ComputeDeckColorIdentity(commander, partner *CardFacts) []string {
	set := map[string]bool{}
	addFrom := func(c *CardFacts) {
		if c == nil {
			return
		}
		src := c.CommanderColorIdentity
		if len(src) == 0 {
			src = c.ColorIdentity
		}
		for _, x := range src {
			set[x] = true
		}
	}
	addFrom(commander)
	addFrom(partner)
	return sortColors(set)
}

func toSet(colors []string) map[string]bool {
	s := map[string]bool{}
	for _, c := range colors {
		s[c] = true
	}
	return s
}

// outside returns the colours in identity that are not in the deck set, in
// WUBRG order.
func outside(identity []string, deck map[string]bool) []string {
	seen := map[string]bool{}
	for _, c := range identity {
		if !deck[c] {
			seen[c] = true
		}
	}
	return sortColors(seen)
}

func sortColors(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		oi, iok := wubrgOrder[out[i]]
		oj, jok := wubrgOrder[out[j]]
		if iok && jok {
			return oi < oj
		}
		if iok != jok {
			return iok
		}
		return out[i] < out[j]
	})
	return out
}
