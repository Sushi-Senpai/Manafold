package synergy

import (
	"crypto/sha256"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// commanderCategory is the Archidekt category a deck files its commander under.
const commanderCategory = "Commander"

// tallyDeck reduces one Archidekt deck to the sorted, distinct oracle ids it
// plays. Quantity is ignored. Excluded: deleted entries, entries in any
// category the deck marks includedInDeck=false (the maybeboard and friends),
// entries in the Commander category, the sampled commander itself even when
// untagged, basic lands, and entries whose uid is not a UUID.
//
// @spec SYN-012
func tallyDeck(d deckDetail, commanderOracle string) []string {
	outOfDeck := make(map[string]bool)
	for _, cat := range d.Categories {
		if !cat.IncludedInDeck {
			outOfDeck[strings.ToLower(cat.Name)] = true
		}
	}
	commanderOracle = strings.ToLower(commanderOracle)

	seen := make(map[string]bool)
	var ids []string
	for _, e := range d.Cards {
		if e.DeletedAt != nil {
			continue
		}
		if excludedByCategory(e.Categories, outOfDeck) {
			continue
		}
		oc := e.Card.OracleCard
		if isBasic(oc.SuperTypes) {
			continue
		}
		uid := strings.ToLower(oc.UID)
		if uid == "" || uid == commanderOracle || seen[uid] {
			continue
		}
		if _, ok := parseUUID(uid); !ok {
			continue
		}
		seen[uid] = true
		ids = append(ids, uid)
	}
	sort.Strings(ids)
	return ids
}

func excludedByCategory(cats []string, outOfDeck map[string]bool) bool {
	for _, c := range cats {
		if strings.EqualFold(c, commanderCategory) || outOfDeck[strings.ToLower(c)] {
			return true
		}
	}
	return false
}

func isBasic(superTypes []string) bool {
	for _, t := range superTypes {
		if strings.EqualFold(t, "Basic") {
			return true
		}
	}
	return false
}

// sample accumulates one target's tallied decks: how many decks were counted,
// how many identical decklists were skipped, and per oracle id how many
// counted decks play it. It lives in memory only while its target refreshes;
// no deck identity is kept beyond the dedup hashes (SYN-020).
type sample struct {
	decks      int
	duplicates int
	cardDecks  map[string]int
	hashes     map[[sha256.Size]byte]bool
}

func newSample() *sample {
	return &sample{cardDecks: make(map[string]int), hashes: make(map[[sha256.Size]byte]bool)}
}

// add counts one tallied deck unless it is empty or identical to a deck
// already counted in this sample (SYN-013).
//
// @spec SYN-013
func (s *sample) add(ids []string) {
	if len(ids) == 0 {
		return
	}
	h := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	if s.hashes[h] {
		s.duplicates++
		return
	}
	s.hashes[h] = true
	s.decks++
	for _, id := range ids {
		s.cardDecks[id]++
	}
}

// inclusionRate is the fraction of counted decks that play the card.
func (s *sample) inclusionRate(id string) float64 {
	if s.decks == 0 {
		return 0
	}
	return float64(s.cardDecks[id]) / float64(s.decks)
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, u.Valid
}

// canonicalIdentity renders a colour identity as WUBRG-ordered letters, "C"
// for colourless — the key a commander and its baseline share.
func canonicalIdentity(colors []string) string {
	var b strings.Builder
	for _, c := range "WUBRG" {
		for _, have := range colors {
			if strings.EqualFold(have, string(c)) {
				b.WriteRune(c)
				break
			}
		}
	}
	if b.Len() == 0 {
		return "C"
	}
	return b.String()
}

// identityArray is the inverse of canonicalIdentity: the text[] the mirror
// stores ("C" → empty).
func identityArray(identity string) []string {
	out := []string{}
	if identity == "C" {
		return out
	}
	for _, c := range identity {
		out = append(out, string(c))
	}
	return out
}
