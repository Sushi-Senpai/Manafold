package cardsync

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
)

// scryfallObject is the subset of a Scryfall card object cardsync reads. Oracle
// Cards and Default Cards share this schema; the Oracle path reads the rules
// fields, the printing path reads the set/art/price fields.
type scryfallObject struct {
	ID              string          `json:"id"`
	OracleID        string          `json:"oracle_id"`
	Name            string          `json:"name"`
	ManaCost        *string         `json:"mana_cost"`
	CMC             float64         `json:"cmc"`
	TypeLine        string          `json:"type_line"`
	OracleText      string          `json:"oracle_text"`
	Colors          []string        `json:"colors"`
	ColorIdentity   []string        `json:"color_identity"`
	ProducedMana    []string        `json:"produced_mana"`
	Keywords        []string        `json:"keywords"`
	Power           *string         `json:"power"`
	Toughness       *string         `json:"toughness"`
	Loyalty         *string         `json:"loyalty"`
	Legalities      json.RawMessage `json:"legalities"`
	GameChanger     bool            `json:"game_changer"`
	Reserved        bool            `json:"reserved"`
	Layout          string          `json:"layout"`
	CardFaces       json.RawMessage `json:"card_faces"`
	EDHRECRank      *int32          `json:"edhrec_rank"`
	Set             string          `json:"set"`
	SetName         string          `json:"set_name"`
	CollectorNumber string          `json:"collector_number"`
	Rarity          string          `json:"rarity"`
	ReleasedAt      string          `json:"released_at"`
	Finishes        []string        `json:"finishes"`
	ImageURIs       json.RawMessage `json:"image_uris"`
	Prices          json.RawMessage `json:"prices"`
	Promo           bool            `json:"promo"`
	Reprint         bool            `json:"reprint"`
	Digital         bool            `json:"digital"`
}

type cardFace struct {
	TypeLine   string `json:"type_line"`
	OracleText string `json:"oracle_text"`
}

func (o scryfallObject) faces() []cardFace {
	if len(o.CardFaces) == 0 {
		return nil
	}
	var faces []cardFace
	_ = json.Unmarshal(o.CardFaces, &faces)
	return faces
}

func (o scryfallObject) toUpsertCardParams() (db.UpsertCardParams, error) {
	oracleUUID, ok := parseUUID(o.OracleID)
	if !ok {
		return db.UpsertCardParams{}, &parseError{what: "oracle_id", value: o.OracleID}
	}
	layout := o.Layout
	if layout == "" {
		layout = "normal"
	}
	legalities := o.Legalities
	if len(legalities) == 0 {
		legalities = json.RawMessage(`{}`)
	}
	return db.UpsertCardParams{
		ScryfallOracleID:       oracleUUID,
		Name:                   o.Name,
		ManaCost:               textPtr(o.ManaCost),
		ManaValue:              o.CMC,
		TypeLine:               o.TypeLine,
		OracleText:             o.OracleText,
		Colors:                 orEmpty(o.Colors),
		ColorIdentity:          orEmpty(o.ColorIdentity), // stored verbatim, never recomputed (CARD-002)
		ProducedMana:           orEmpty(o.ProducedMana),
		Keywords:               orEmpty(o.Keywords),
		Power:                  textPtr(o.Power),
		Toughness:              textPtr(o.Toughness),
		Loyalty:                textPtr(o.Loyalty),
		Legalities:             legalities,
		IsGameChanger:          o.GameChanger,
		IsReserved:             o.Reserved,
		Layout:                 layout,
		CardFaces:              []byte(o.CardFaces),
		SingletonLimit:         deriveSingletonLimit(o.OracleText),
		CanBeCommander:         deriveCanBeCommander(o.TypeLine, o.OracleText, o.faces()),
		CommanderColorIdentity: append([]string{}, o.ColorIdentity...),
		EdhrecRank:             int4Ptr(o.EDHRECRank),
	}, nil
}

func (o scryfallObject) toUpsertCardPrintParams(cardID pgtype.UUID) db.UpsertCardPrintParams {
	scryfallUUID, _ := parseUUID(o.ID)
	return db.UpsertCardPrintParams{
		ScryfallID:      scryfallUUID,
		CardID:          cardID,
		SetCode:         o.Set,
		SetName:         o.SetName,
		CollectorNumber: o.CollectorNumber,
		Rarity:          o.Rarity,
		ReleasedAt:      dateFrom(o.ReleasedAt),
		Finishes:        orEmpty(o.Finishes),
		ImageUris:       []byte(o.ImageURIs),
		Prices:          []byte(o.Prices),
		IsPromo:         o.Promo,
		IsReprint:       o.Reprint,
		IsDigital:       o.Digital,
	}
}

type parseError struct {
	what  string
	value string
}

func (e *parseError) Error() string {
	return "cardsync: " + e.what + " is not a valid uuid: " + strconv.Quote(e.value)
}

var numberWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
	"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
}

// upToLimitRe captures the count in "A deck can have up to N cards named ...".
var upToLimitRe = regexp.MustCompile(`a deck can have up to (\w+) cards named`)

// deriveSingletonLimit reads the "deck can have ... cards named" clause from a
// card's Oracle text (CARD-003):
//   - "any number of" -> 0 (unlimited)
//   - "up to N"       -> N
//   - otherwise       -> NULL (normal singleton)
//
// The "cards named" guard keeps unrelated "up to N target ..." wording from
// matching.
func deriveSingletonLimit(oracleText string) pgtype.Int4 {
	lower := strings.ToLower(oracleText)
	if !strings.Contains(lower, "cards named") {
		return pgtype.Int4{}
	}
	if strings.Contains(lower, "any number of cards named") {
		return pgtype.Int4{Int32: 0, Valid: true}
	}
	if m := upToLimitRe.FindStringSubmatch(lower); m != nil {
		word := m[1]
		if n, err := strconv.Atoi(word); err == nil {
			return pgtype.Int4{Int32: int32(n), Valid: true}
		}
		if n, ok := numberWords[word]; ok {
			return pgtype.Int4{Int32: int32(n), Valid: true}
		}
	}
	return pgtype.Int4{}
}

// deriveCanBeCommander is true when the type line carries both "Legendary" and
// "Creature", or the Oracle text (on any face) says "can be your commander"
// (CARD-004).
func deriveCanBeCommander(typeLine, oracleText string, faces []cardFace) bool {
	if grantsCommand(typeLine, oracleText) {
		return true
	}
	for _, f := range faces {
		if grantsCommand(f.TypeLine, f.OracleText) {
			return true
		}
	}
	return false
}

func grantsCommand(typeLine, oracleText string) bool {
	tl := strings.ToLower(typeLine)
	if strings.Contains(tl, "legendary") && strings.Contains(tl, "creature") {
		return true
	}
	return strings.Contains(strings.ToLower(oracleText), "can be your commander")
}
