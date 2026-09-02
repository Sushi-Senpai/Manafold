package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckrules"
)

// callerID returns the authenticated user's ID. The auth middleware fails
// closed, so any handler that runs at all has one.
func callerID(r *http.Request) pgtype.UUID {
	id, _ := authctx.UserID(r.Context())
	return id
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, u.Valid
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func int4Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	n := v.Int32
	return &n
}

// int4IntPtr renders a pgtype.Int4 as the *int deckrules expects for
// singleton_limit (nil = normal singleton).
func int4IntPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

// rawOrNull returns b as a json.RawMessage, or the JSON literal null when b is
// empty, so an absent jsonb column serialises as null rather than "".
func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// isBasicLand reports whether a type line is a basic land, which the singleton
// rule exempts entirely (DECK-006).
func isBasicLand(typeLine string) bool {
	return strings.Contains(typeLine, "Basic") && strings.Contains(typeLine, "Land")
}

// legalitiesCommander pulls the "commander" entry out of a card's legalities
// jsonb map ("legal" | "banned" | "restricted" | "not_legal" | "").
func legalitiesCommander(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	return m["commander"]
}

// cardSummary is the CardSummary shape the frontend's lib/api.ts expects: a
// card's Oracle fields plus its newest printing's image and price data.
type cardSummary struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	ManaCost        *string         `json:"mana_cost"`
	ManaValue       float64         `json:"mana_value"`
	TypeLine        string          `json:"type_line"`
	OracleText      string          `json:"oracle_text"`
	Colors          []string        `json:"colors"`
	ColorIdentity   []string        `json:"color_identity"`
	Keywords        []string        `json:"keywords"`
	CanBeCommander  bool            `json:"can_be_commander"`
	EdhrecRank      *int32          `json:"edhrec_rank"`
	Layout          string          `json:"layout"`
	ImageURIs       json.RawMessage `json:"image_uris"`
	Prices          json.RawMessage `json:"prices"`
	SetCode         string          `json:"set_code"`
	CollectorNumber string          `json:"collector_number"`
}

func cardSummaryFrom(c db.Card, print db.CardPrint, havePrint bool) cardSummary {
	s := cardSummary{
		ID:             uuidString(c.ID),
		Name:           c.Name,
		ManaCost:       textPtr(c.ManaCost),
		ManaValue:      c.ManaValue,
		TypeLine:       c.TypeLine,
		OracleText:     c.OracleText,
		Colors:         nonNil(c.Colors),
		ColorIdentity:  nonNil(c.ColorIdentity),
		Keywords:       nonNil(c.Keywords),
		CanBeCommander: c.CanBeCommander,
		EdhrecRank:     int4Ptr(c.EdhrecRank),
		Layout:         c.Layout,
		ImageURIs:      json.RawMessage("null"),
		Prices:         json.RawMessage("null"),
	}
	if havePrint {
		s.ImageURIs = rawOrNull(print.ImageUris)
		s.Prices = rawOrNull(print.Prices)
		s.SetCode = print.SetCode
		s.CollectorNumber = print.CollectorNumber
	}
	return s
}

// cardFactsFrom builds the deckrules input for a full cards row (used for the
// assigned commander / partner).
func cardFactsFrom(c db.Card, overrideBanned *bool) deckrules.CardFacts {
	return deckrules.CardFacts{
		ID:                     uuidString(c.ID),
		Name:                   c.Name,
		ColorIdentity:          nonNil(c.ColorIdentity),
		CommanderColorIdentity: c.CommanderColorIdentity,
		CanBeCommander:         c.CanBeCommander,
		Keywords:               nonNil(c.Keywords),
		OracleText:             c.OracleText,
		TypeLine:               c.TypeLine,
		SingletonLimit:         int4IntPtr(c.SingletonLimit),
		IsBasicLand:            isBasicLand(c.TypeLine),
		LegalitiesCommander:    legalitiesCommander(c.Legalities),
		OverrideBanned:         overrideBanned,
	}
}

// cardFactsFromEntry builds the deckrules input for one deck_cards entry row.
func cardFactsFromEntry(e db.ListDeckCardEntriesRow, overrideBanned *bool) deckrules.CardFacts {
	return deckrules.CardFacts{
		ID:                  uuidString(e.CardID),
		Name:                e.Name,
		ColorIdentity:       nonNil(e.ColorIdentity),
		CanBeCommander:      e.CanBeCommander,
		Keywords:            nonNil(e.Keywords),
		OracleText:          e.OracleText,
		TypeLine:            e.TypeLine,
		SingletonLimit:      int4IntPtr(e.SingletonLimit),
		IsBasicLand:         isBasicLand(e.TypeLine),
		LegalitiesCommander: e.LegalitiesCommander,
		OverrideBanned:      overrideBanned,
	}
}
