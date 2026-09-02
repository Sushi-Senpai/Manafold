package deckrules

import (
	"fmt"
	"strings"
)

// commanderIssues checks Rule 5 — commander shape. It returns a list of
// human-readable issue strings (empty when the commander configuration is
// valid).
//
// @spec DECK-020, DECK-021
func commanderIssues(commander, partner *CardFacts) []string {
	var issues []string

	if commander == nil {
		issues = append(issues, "no commander assigned")
		// A partner with no primary commander is still worth flagging, but the
		// pairing check below needs both cards, so stop here.
		if partner != nil {
			issues = append(issues, "a partner is set but no primary commander is assigned")
		}
		return issues
	}

	if !commander.CanBeCommander {
		issues = append(issues, fmt.Sprintf("%s cannot be a commander", commander.Name))
	}

	if partner != nil {
		if !partner.CanBeCommander {
			issues = append(issues, fmt.Sprintf("%s cannot be a commander", partner.Name))
		}
		if !compatiblePartners(*commander, *partner) {
			issues = append(issues, fmt.Sprintf(
				"%s and %s are not a valid partner pairing", commander.Name, partner.Name))
		}
	}

	return issues
}

// compatiblePartners reports whether two legendary cards may be designated as a
// deck's two commanders together, covering the five variants: plain Partner,
// "Partner with", Friends forever, Choose a Background, and Doctor's companion.
func compatiblePartners(a, b CardFacts) bool {
	return bothPlainPartner(a, b) ||
		partnerWithEachOther(a, b) ||
		bothFriendsForever(a, b) ||
		backgroundPairing(a, b) ||
		doctorPairing(a, b)
}

func hasKeyword(c CardFacts, kw string) bool {
	for _, k := range c.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

func textContains(c CardFacts, phrase string) bool {
	return strings.Contains(strings.ToLower(c.OracleText), strings.ToLower(phrase))
}

func typeContains(c CardFacts, phrase string) bool {
	return strings.Contains(strings.ToLower(c.TypeLine), strings.ToLower(phrase))
}

// bothPlainPartner: each card has the bare "Partner" ability and neither is a
// "Partner with" card (those only pair with their named counterpart).
func bothPlainPartner(a, b CardFacts) bool {
	return plainPartner(a) && plainPartner(b)
}

func plainPartner(c CardFacts) bool {
	if partnerWithName(c) != "" {
		return false
	}
	if hasKeyword(c, "Partner") {
		return true
	}
	// Reminder text form, e.g. "Partner (You can have two commanders if both
	// have partner.)"
	return strings.Contains(strings.ToLower(c.OracleText), "partner (")
}

// partnerWithName returns the card named by a "Partner with <name>" ability, or
// "" if the card has no such ability.
func partnerWithName(c CardFacts) string {
	lower := strings.ToLower(c.OracleText)
	idx := strings.Index(lower, "partner with ")
	if idx < 0 {
		return ""
	}
	rest := c.OracleText[idx+len("partner with "):]
	// The name runs to the end of the clause — a period, an opening paren, or a
	// newline.
	end := strings.IndexAny(rest, ".(\n")
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func partnerWithEachOther(a, b CardFacts) bool {
	an, bn := partnerWithName(a), partnerWithName(b)
	if an == "" || bn == "" {
		return false
	}
	return strings.EqualFold(an, b.Name) && strings.EqualFold(bn, a.Name)
}

func bothFriendsForever(a, b CardFacts) bool {
	return (hasKeyword(a, "Friends forever") || textContains(a, "friends forever")) &&
		(hasKeyword(b, "Friends forever") || textContains(b, "friends forever"))
}

// backgroundPairing: one card says "Choose a Background" and the other is a
// Background enchantment.
func backgroundPairing(a, b CardFacts) bool {
	return (textContains(a, "choose a background") && typeContains(b, "background")) ||
		(textContains(b, "choose a background") && typeContains(a, "background"))
}

// doctorPairing: one card is a Time Lord Doctor and the other has Doctor's
// companion.
func doctorPairing(a, b CardFacts) bool {
	return (typeContains(a, "time lord doctor") && textContains(b, "doctor's companion")) ||
		(typeContains(b, "time lord doctor") && textContains(a, "doctor's companion"))
}
