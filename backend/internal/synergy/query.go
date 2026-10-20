package synergy

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
)

// Attribution every surface showing synergy data must carry — the link back
// Archidekt's developer asked for (SYN-040).
const (
	AttributionText = "Synergy data from public Archidekt decks"
	AttributionURL  = "https://archidekt.com"
)

// DefaultTopCards / MaxTopCards bound the TopCards limit.
const (
	DefaultTopCards = 30
	MaxTopCards     = 100
)

// ErrNoSample means the commander has no stored synergy sample (SYN-042).
var ErrNoSample = errors.New("no synergy sample for this commander")

// CommanderSynergy is one commander's sample metadata and top cards.
type CommanderSynergy struct {
	Sample db.GetSynergySampleRow
	Cards  []db.ListTopSynergyCardsRow
}

// TopCards returns a commander's top synergy cards from the local database —
// mirror rows only, commander-legal with banlist overrides applied — plus the
// sample they come from. commanderCardID is the commander's cards.id. limit is
// clamped to [1, MaxTopCards], 0 meaning DefaultTopCards. It never calls
// Archidekt (SYN-040, SYN-041, SYN-042).
//
// @spec SYN-040, SYN-041, SYN-042
func TopCards(ctx context.Context, q *db.Queries, commanderCardID pgtype.UUID, limit int) (CommanderSynergy, error) {
	switch {
	case limit <= 0:
		limit = DefaultTopCards
	case limit > MaxTopCards:
		limit = MaxTopCards
	}
	sample, err := q.GetSynergySample(ctx, commanderCardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommanderSynergy{}, ErrNoSample
	}
	if err != nil {
		return CommanderSynergy{}, err
	}
	cards, err := q.ListTopSynergyCards(ctx, db.ListTopSynergyCardsParams{
		CommanderOracleID: sample.CommanderOracleID,
		Lim:               int32(limit),
	})
	if err != nil {
		return CommanderSynergy{}, err
	}
	return CommanderSynergy{Sample: sample, Cards: cards}, nil
}
