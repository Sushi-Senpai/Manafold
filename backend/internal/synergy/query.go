package synergy

// Attribution every surface showing synergy data must carry — the link back
// Archidekt's developer asked for (SYN-040, SYN-043).
const (
	AttributionText = "Synergy data from public Archidekt decks"
	AttributionURL  = "https://archidekt.com"
)

// DefaultTopCards / MaxTopCards bound the card limit of Service.Commander.
const (
	DefaultTopCards = 30
	MaxTopCards     = 100
)

// clampLimit applies the default and maximum card limits.
func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultTopCards
	case limit > MaxTopCards:
		return MaxTopCards
	}
	return limit
}
