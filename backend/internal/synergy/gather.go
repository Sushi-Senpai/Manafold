// Package synergy computes per-commander card synergy from public Archidekt
// decklists and serves it from Manafold's own Postgres. It is gathered on
// demand: a user building for a commander asks the Service to Ensure it, which
// only queues a job; one background worker (RunWorker) drains the queue
// through a single Archidekt client paced at one request per 6 s (SYN-003,
// SYN-030..036). The API and the deck wizard read the result with Service.Commander
// and Service.Status.
//
// A job samples the commander's colour-identity baseline if it is not fresh,
// then the commander. Each deck is reduced to the set of oracle ids it plays
// (tallyDeck), identical decklists count once (SYN-012, SYN-013), and only
// aggregates are written, one target per transaction (SYN-020, SYN-022).
// Synergy is commander inclusion minus baseline inclusion (SYN-021). See
// docs/intent/synergy/.
//
// @spec SYN-010, SYN-011, SYN-020, SYN-022
package synergy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

// retryAllowance is the number of extra requests a job may spend on 429
// retries beyond its targets' worst case (SYN-034).
const retryAllowance = 10

// commanderRef is one commander resolved against the mirror.
type commanderRef struct {
	oracleID pgtype.UUID
	oracle   string // lower-case UUID string, as Archidekt's oracleCard.uid
	name     string
	identity string // canonical, see CanonicalIdentity
}

// target is one sample the worker takes: a commander's, or the baseline for
// one colour identity drawn from that identity's baseline commanders.
type target struct {
	baseline  bool
	identity  string
	commander commanderRef   // commander targets
	sources   []commanderRef // baseline targets: whose decks form the sample
}

func (t target) label() string {
	if t.baseline {
		return "baseline " + t.identity
	}
	return "commander " + t.commander.name
}

// worstCase is the most requests the target can make without a 429 retry.
func (t target) worstCase(o Options) int {
	if t.baseline {
		return len(t.sources) * (o.SearchPages + o.DecksPerBaselineCommander)
	}
	return o.SearchPages + o.DecksPerCommander
}

// sampleTarget fetches and tallies one target's whole sample. A baseline
// dedups across all its source commanders' decks.
func sampleTarget(ctx context.Context, c *archidektClient, t target, o Options) (*sample, error) {
	s := newSample()
	if !t.baseline {
		return s, sampleCommanderDecks(ctx, c, t.commander, o.DecksPerCommander, o.SearchPages, s)
	}
	for _, src := range t.sources {
		if err := sampleCommanderDecks(ctx, c, src, o.DecksPerBaselineCommander, o.SearchPages, s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// sampleCommanderDecks collects up to want candidate decks — public, Commander
// format, at least minDeckSize cards — from at most pages search pages, newest
// first, then fetches and tallies each into s (SYN-010). A deck id the search
// returns twice is fetched once, and a deck that vanished between search and
// fetch is skipped (SYN-005).
//
// @spec SYN-010, SYN-005
func sampleCommanderDecks(ctx context.Context, c *archidektClient, cmd commanderRef, want, pages int, s *sample) error {
	searchName := frontFace(cmd.name)
	var ids []int64
	seen := make(map[int64]bool)
	for page := 1; page <= pages && len(ids) < want; page++ {
		sp, err := c.search(ctx, searchName, page)
		if err != nil {
			return err
		}
		for _, r := range sp.Results {
			if isCandidate(r) && !seen[r.ID] {
				seen[r.ID] = true
				ids = append(ids, r.ID)
				if len(ids) == want {
					break
				}
			}
		}
		if sp.Next == nil || len(sp.Results) == 0 {
			break
		}
	}
	for _, id := range ids {
		d, found, err := c.deck(ctx, id)
		if err != nil {
			return err
		}
		if !found || d.Private || d.Unlisted || d.DeckFormat != commanderFormat {
			continue
		}
		s.add(tallyDeck(d, cmd.oracle))
	}
	return nil
}

func isCandidate(r searchResult) bool {
	return !r.Private && !r.Unlisted && r.DeckFormat == commanderFormat && r.Size >= minDeckSize
}

// frontFace returns the front face of a multi-face card name ("A // B" → "A"),
// the name Archidekt's commanderName search matches.
func frontFace(name string) string {
	if i := strings.Index(name, " // "); i >= 0 {
		return name[:i]
	}
	return name
}

// writeTarget replaces one target's aggregates and recomputes synergy for its
// colour identity in a single transaction (SYN-020, SYN-021, SYN-022).
//
// @spec SYN-020, SYN-021, SYN-022
func writeTarget(ctx context.Context, pool *pgxpool.Pool, t target, s *sample, fetchedAt time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	at := pgtype.Timestamptz{Time: fetchedAt, Valid: true}

	cardIDs := make([]string, 0, len(s.cardDecks))
	for id := range s.cardDecks {
		cardIDs = append(cardIDs, id)
	}
	sort.Strings(cardIDs)

	if t.baseline {
		names := make([]string, len(t.sources))
		for i, src := range t.sources {
			names[i] = src.name
		}
		if err := q.UpsertSynergyBaseline(ctx, db.UpsertSynergyBaselineParams{
			ColorIdentity:      t.identity,
			BaselineCommanders: names,
			DeckCount:          int32(s.decks),
			DuplicatesSkipped:  int32(s.duplicates),
			FetchedAt:          at,
		}); err != nil {
			return err
		}
		if err := q.DeleteSynergyBaselineCards(ctx, t.identity); err != nil {
			return err
		}
		rows := make([]db.InsertSynergyBaselineCardsParams, 0, len(cardIDs))
		for _, id := range cardIDs {
			u, _ := parseUUID(id)
			rows = append(rows, db.InsertSynergyBaselineCardsParams{
				ColorIdentity: t.identity,
				CardOracleID:  u,
				DeckCount:     int32(s.cardDecks[id]),
				InclusionRate: s.inclusionRate(id),
			})
		}
		if _, err := q.InsertSynergyBaselineCards(ctx, rows); err != nil {
			return err
		}
	} else {
		if err := q.UpsertSynergyCommander(ctx, db.UpsertSynergyCommanderParams{
			CommanderOracleID: t.commander.oracleID,
			CommanderName:     t.commander.name,
			ColorIdentity:     t.identity,
			DeckCount:         int32(s.decks),
			DuplicatesSkipped: int32(s.duplicates),
			FetchedAt:         at,
		}); err != nil {
			return err
		}
		if err := q.DeleteSynergyCommanderCards(ctx, t.commander.oracleID); err != nil {
			return err
		}
		rows := make([]db.InsertSynergyCommanderCardsParams, 0, len(cardIDs))
		for _, id := range cardIDs {
			u, _ := parseUUID(id)
			rows = append(rows, db.InsertSynergyCommanderCardsParams{
				CommanderOracleID: t.commander.oracleID,
				CardOracleID:      u,
				DeckCount:         int32(s.cardDecks[id]),
				InclusionRate:     s.inclusionRate(id),
			})
		}
		if _, err := q.InsertSynergyCommanderCards(ctx, rows); err != nil {
			return err
		}
	}

	if err := q.RecomputeSynergyForIdentity(ctx, t.identity); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
