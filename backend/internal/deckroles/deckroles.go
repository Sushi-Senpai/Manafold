// Package deckroles assigns functional roles — ramp, card draw, targeted
// removal, board wipe — to cards, for counting a deck against its archetype
// template (internal/decktemplates). Everything that counts roles depends only
// on the Classifier interface, so the deterministic Oracle-text default here can
// later be swapped for one backed by Scryfall's community role tags (CARD-010)
// without touching the template-fit endpoint or the builder panel. Pure: no DB,
// no AI. The rules and their known limits are documented in
// docs/intent/deck-building/deck-building-design.md § internal/deckroles.
//
// @spec DECK-106, DECK-107
package deckroles

import (
	"regexp"
	"strings"
)

// Role is one functional role a card can fill. The string values double as the
// template row keys in internal/decktemplates.
type Role string

const (
	Ramp            Role = "ramp"
	CardDraw        Role = "card_draw"
	TargetedRemoval Role = "targeted_removal"
	BoardWipe       Role = "board_wipe"
)

// Roles lists every role in display order.
var Roles = []Role{Ramp, CardDraw, TargetedRemoval, BoardWipe}

// Face is one face of a multi-face card, as stored in cards.card_faces.
type Face struct {
	TypeLine   string `json:"type_line"`
	OracleText string `json:"oracle_text"`
}

// Card is the classifier input: the Oracle fields of one card.
type Card struct {
	TypeLine   string
	OracleText string
	Keywords   []string
	Faces      []Face
}

// Classifier assigns roles to a card. A card may fill several roles, or none.
type Classifier interface {
	Classify(c Card) []Role
}

// Entry is one deck entry to count: a card and its quantity.
type Entry struct {
	Card     Card
	Quantity int
}

// Count tallies, per role, the summed quantity of every entry the classifier
// assigns that role. A card filling two roles counts toward both.
//
// @spec DECK-106
func Count(cl Classifier, entries []Entry) map[Role]int {
	out := make(map[Role]int, len(Roles))
	for _, r := range Roles {
		out[r] = 0
	}
	for _, e := range entries {
		qty := e.Quantity
		if qty <= 0 {
			qty = 1
		}
		for _, r := range cl.Classify(e.Card) {
			out[r] += qty
		}
	}
	return out
}

// OracleClassifier is the deterministic default: conservative pattern rules
// over a card's Oracle text and type line. A miss is preferred over a false
// positive.
type OracleClassifier struct{}

var (
	reminderText = regexp.MustCompile(`\([^)]*\)`)

	// Ramp.
	manaAbility = regexp.MustCompile(`\badd (\{[wubrgcs0-9x]\}|one mana|two mana|three mana|x mana|an amount of|that much mana)`)
	landSearch  = regexp.MustCompile(`search your library for [^.]*?\bland cards?\b[^.]*?onto the battlefield`)
	handLand    = regexp.MustCompile(`put (a|an|up to \w+) (basic )?land cards? from your hand onto the battlefield`)
	extraLand   = regexp.MustCompile(`play (an|two|three) additional lands?`)
	treasure    = regexp.MustCompile(`\bcreates? [^.]*?\btreasure tokens?\b`)

	// Card draw.
	drawCards = regexp.MustCompile(`\bdraw (a card|an additional card|cards equal|that many cards|\w+ cards)`)

	// Targeted removal.
	targetedKill   = regexp.MustCompile(`\b(destroy|exile) (up to (one|two|three|x) )?(other )?(another )?target ([^.,;]*)`)
	targetedBounce = regexp.MustCompile(`\breturn (up to (one|two|three) )?(another )?target ([^.;]*?) to (its|their) owner'?s'? hands?`)
	targetedBurn   = regexp.MustCompile(`damage to (any (other )?target|target (\w+ )?(creature|planeswalker|battle))`)
	targetedShrink = regexp.MustCompile(`target creature[^.]*? gets -(\d+|x)/-(\d+|x)`)

	// Board wipes.
	massKill   = regexp.MustCompile(`\b(destroy|exile) (all|each) ([^.;]*)`)
	massBounce = regexp.MustCompile(`\breturn (all|each) [^.;]*? to (its|their) owner'?s'? hands?`)
	massBurn   = regexp.MustCompile(`damage to each (\w+ )?creature`)
	massShrink = regexp.MustCompile(`all (\w+ )?creatures get -(\d+|x)/-(\d+|x)`)

	permanentNoun = regexp.MustCompile(`\b(creatures?|artifacts?|enchantments?|planeswalkers?|battles?|permanents?)\b`)
	cardNoun      = regexp.MustCompile(`\bcards?\b`)
)

// Classify returns the roles the card fills, in Roles order.
//
// @spec DECK-107
func (OracleClassifier) Classify(c Card) []Role {
	typeLine, text := frontFace(c)
	if strings.Contains(typeLine, "Land") {
		return nil
	}
	t := strings.ToLower(reminderText.ReplaceAllString(text, ""))
	permanent := !strings.Contains(typeLine, "Instant") && !strings.Contains(typeLine, "Sorcery")

	var roles []Role
	if isRamp(t, permanent) {
		roles = append(roles, Ramp)
	}
	if isDraw(t) {
		roles = append(roles, CardDraw)
	}
	removal := isTargetedRemoval(t)
	if removal {
		roles = append(roles, TargetedRemoval)
	}
	if isBoardWipe(t) || (removal && hasKeyword(c.Keywords, "Overload")) {
		roles = append(roles, BoardWipe)
	}
	return roles
}

// frontFace returns the type line and Oracle text to classify: the top-level
// fields, or the front face's when the top-level Oracle text is empty (multi-face
// layouts store their text per face).
func frontFace(c Card) (string, string) {
	if strings.TrimSpace(c.OracleText) == "" && len(c.Faces) > 0 {
		return c.Faces[0].TypeLine, c.Faces[0].OracleText
	}
	return c.TypeLine, c.OracleText
}

func isRamp(t string, permanent bool) bool {
	if permanent && manaAbility.MatchString(t) {
		return true
	}
	return landSearch.MatchString(t) || handLand.MatchString(t) ||
		extraLand.MatchString(t) || treasure.MatchString(t)
}

// isDraw matches the imperative "draw a card / N cards" (the caller drawing),
// skipping "whenever you draw" / "when you draw" triggers and "would draw"
// replacement effects. "Draws" (another player drawing) never matches.
func isDraw(t string) bool {
	for _, loc := range drawCards.FindAllStringIndex(t, -1) {
		before := t[:loc[0]]
		if strings.HasSuffix(before, "whenever you ") || strings.HasSuffix(before, "when you ") ||
			strings.HasSuffix(before, "would ") {
			continue
		}
		return true
	}
	return false
}

func isTargetedRemoval(t string) bool {
	for _, m := range targetedKill.FindAllStringSubmatch(t, -1) {
		if removablePermanent(m[6]) {
			return true
		}
	}
	for _, m := range targetedBounce.FindAllStringSubmatch(t, -1) {
		if removablePermanent(m[4]) {
			return true
		}
	}
	return targetedBurn.MatchString(t) || targetedShrink.MatchString(t)
}

// removablePermanent reports whether a targeted clause's object is a permanent
// the caller does not control — not a card in a graveyard or library, and not
// the caller's own permanent (a blink or sacrifice outlet).
func removablePermanent(object string) bool {
	if !permanentNoun.MatchString(object) || cardNoun.MatchString(object) {
		return false
	}
	return !strings.Contains(object, "you control") || strings.Contains(object, "you don't control")
}

func isBoardWipe(t string) bool {
	for _, m := range massKill.FindAllStringSubmatch(t, -1) {
		if permanentNoun.MatchString(m[3]) && !cardNoun.MatchString(m[3]) {
			return true
		}
	}
	return massBounce.MatchString(t) || massBurn.MatchString(t) || massShrink.MatchString(t)
}

func hasKeyword(keywords []string, want string) bool {
	for _, k := range keywords {
		if strings.EqualFold(k, want) {
			return true
		}
	}
	return false
}
