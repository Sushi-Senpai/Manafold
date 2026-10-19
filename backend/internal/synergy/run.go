// Package synergy computes per-commander card synergy from public Archidekt
// decklists and serves it from Manafold's own Postgres. cmd/synergysync calls
// Run; the API handler and (later) the deck wizard call TopCards.
//
// A run refreshes targets — one sample per configured commander plus one
// colour-identity baseline per distinct commander identity — reading only
// Archidekt's public search and deck endpoints at no more than one request per
// 6 s (SYN-002, SYN-003). Each deck is reduced to the set of oracle ids it
// plays (tallyDeck), identical decklists count once (SYN-012, SYN-013), and
// only aggregates are written, one target per transaction (SYN-020, SYN-022).
// Synergy is commander inclusion minus baseline inclusion (SYN-021). Runs are
// bounded by a request budget and resume by target staleness (SYN-030,
// SYN-031). See docs/intent/synergy/.
//
// @spec SYN-010, SYN-011, SYN-020, SYN-022, SYN-030, SYN-031, SYN-032
package synergy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

// Options configures a run. Zero values take the defaults in withDefaults.
type Options struct {
	// BaseURL replaces https://archidekt.com (tests point it at a fixture
	// server). HTTPClient replaces the default client.
	BaseURL    string
	HTTPClient *http.Client

	// Commanders is the configured commander list, by card name (SYN-001).
	Commanders []string

	DecksPerCommander         int           // default 60
	SearchPages               int           // default 3
	BaselineCommanders        int           // default 5
	DecksPerBaselineCommander int           // default 12
	MaxRequests               int           // default 600
	RefreshAfter              time.Duration // default 7 days

	// Logf receives progress lines; nil means log.Printf.
	Logf func(format string, args ...any)
}

func (o Options) withDefaults() Options {
	if o.DecksPerCommander <= 0 {
		o.DecksPerCommander = 60
	}
	if o.SearchPages <= 0 {
		o.SearchPages = 3
	}
	if o.BaselineCommanders <= 0 {
		o.BaselineCommanders = 5
	}
	if o.DecksPerBaselineCommander <= 0 {
		o.DecksPerBaselineCommander = 12
	}
	if o.MaxRequests <= 0 {
		o.MaxRequests = 600
	}
	if o.RefreshAfter <= 0 {
		o.RefreshAfter = 7 * 24 * time.Hour
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	return o
}

// Result reports what a run did.
type Result struct {
	RequestsMade     int
	TargetsRefreshed int
	TargetsPending   int
	Unresolved       []string // configured names that did not resolve to a commander
}

// commanderRef is one commander resolved against the mirror.
type commanderRef struct {
	oracleID pgtype.UUID
	oracle   string // lower-case UUID string, as Archidekt's oracleCard.uid
	name     string
	identity string // canonical, see canonicalIdentity
}

// target is one unit of refresh work: a commander sample, or the baseline for
// one colour identity drawn from that identity's baseline commanders.
type target struct {
	baseline    bool
	identity    string
	commander   commanderRef   // commander targets
	sources     []commanderRef // baseline targets: whose decks form the sample
	lastFetched time.Time      // zero when never sampled
}

func (t target) label() string {
	if t.baseline {
		return "baseline " + t.identity
	}
	return "commander " + t.commander.name
}

// worstCase is the most requests the target can make without a 429 retry; a
// target starts only when the budget covers it (SYN-030).
func (t target) worstCase(o Options) int {
	if t.baseline {
		return len(t.sources) * (o.SearchPages + o.DecksPerBaselineCommander)
	}
	return o.SearchPages + o.DecksPerCommander
}

// Run refreshes every stale target, stalest first, until the targets or the
// request budget run out, and records a synergy_runs row (SYN-032). A run
// stopped by the budget succeeds with the rest pending; any other error marks
// the run failed and is returned. Targets refreshed before the stop keep their
// new aggregates.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (Result, error) {
	opts = opts.withDefaults()
	q := db.New(pool)
	var res Result

	runID, err := q.CreateSynergyRun(ctx)
	if err != nil {
		return res, err
	}
	client := newArchidektClient(opts.BaseURL, opts.HTTPClient, opts.MaxRequests)

	finish := func(runErr error) error {
		res.RequestsMade = client.requests
		params := db.FinishSynergyRunParams{
			ID:               runID,
			Status:           "succeeded",
			RequestsMade:     int32(res.RequestsMade),
			TargetsRefreshed: int32(res.TargetsRefreshed),
			TargetsPending:   int32(res.TargetsPending),
		}
		if runErr != nil {
			params.Status = "failed"
			params.Error = pgtype.Text{String: runErr.Error(), Valid: true}
		}
		// Record the outcome even when ctx was cancelled mid-run.
		if err := q.FinishSynergyRun(context.WithoutCancel(ctx), params); err != nil && runErr == nil {
			return err
		}
		return runErr
	}

	targets, unresolved, err := plan(ctx, q, opts)
	res.Unresolved = unresolved
	if err != nil {
		return res, finish(err)
	}

	for i, t := range targets {
		if client.remaining() < t.worstCase(opts) {
			res.TargetsPending = len(targets) - i
			opts.Logf("synergy: request budget left (%d) does not cover %s; %d target(s) pending",
				client.remaining(), t.label(), res.TargetsPending)
			break
		}
		s, err := sampleTarget(ctx, client, t, opts)
		if errors.Is(err, errBudgetExhausted) {
			res.TargetsPending = len(targets) - i
			opts.Logf("synergy: request budget spent during %s; %d target(s) pending", t.label(), res.TargetsPending)
			break
		}
		if err != nil {
			res.TargetsPending = len(targets) - i
			return res, finish(fmt.Errorf("%s: %w", t.label(), err))
		}
		if err := writeTarget(ctx, pool, t, s, time.Now()); err != nil {
			res.TargetsPending = len(targets) - i
			return res, finish(fmt.Errorf("%s: write: %w", t.label(), err))
		}
		res.TargetsRefreshed++
		opts.Logf("synergy: refreshed %s: %d decks (%d duplicates skipped), %d cards",
			t.label(), s.decks, s.duplicates, len(s.cardDecks))
	}
	return res, finish(nil)
}

// plan resolves the commander list, builds the commander and baseline targets,
// keeps the stale ones, and orders them stalest first — never-sampled first,
// baselines before commanders on a tie (SYN-001, SYN-011, SYN-031).
func plan(ctx context.Context, q *db.Queries, opts Options) ([]target, []string, error) {
	var commanders []commanderRef
	var unresolved []string
	seen := make(map[string]bool)
	for _, name := range opts.Commanders {
		row, err := q.ResolveSynergyCommander(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			opts.Logf("synergy: commander %q not found in the card mirror; skipping", name)
			unresolved = append(unresolved, name)
			continue
		}
		if err != nil {
			return nil, unresolved, err
		}
		ref := commanderRef{
			oracleID: row.ScryfallOracleID,
			oracle:   uuidString(row.ScryfallOracleID),
			name:     row.Name,
			identity: canonicalIdentity(row.Identity),
		}
		if seen[ref.oracle] {
			continue
		}
		seen[ref.oracle] = true
		commanders = append(commanders, ref)
	}

	commanderFetched, err := q.ListSynergyCommanderFetchedAt(ctx)
	if err != nil {
		return nil, unresolved, err
	}
	lastCommander := make(map[string]time.Time, len(commanderFetched))
	for _, r := range commanderFetched {
		lastCommander[uuidString(r.CommanderOracleID)] = r.FetchedAt.Time
	}
	baselineFetched, err := q.ListSynergyBaselineFetchedAt(ctx)
	if err != nil {
		return nil, unresolved, err
	}
	lastBaseline := make(map[string]time.Time, len(baselineFetched))
	for _, r := range baselineFetched {
		lastBaseline[r.ColorIdentity] = r.FetchedAt.Time
	}

	staleBefore := time.Now().Add(-opts.RefreshAfter)
	isStale := func(t time.Time) bool { return t.IsZero() || t.Before(staleBefore) }

	exclude := make([]pgtype.UUID, len(commanders))
	for i, c := range commanders {
		exclude[i] = c.oracleID
	}

	var targets []target
	identities := make(map[string]bool)
	for _, c := range commanders {
		if !identities[c.identity] {
			identities[c.identity] = true
			if last := lastBaseline[c.identity]; isStale(last) {
				rows, err := q.ListBaselineCommanders(ctx, db.ListBaselineCommandersParams{
					Identity: identityArray(c.identity),
					Exclude:  exclude,
					Lim:      int32(opts.BaselineCommanders),
				})
				if err != nil {
					return nil, unresolved, err
				}
				t := target{baseline: true, identity: c.identity, lastFetched: last}
				for _, r := range rows {
					t.sources = append(t.sources, commanderRef{
						oracleID: r.ScryfallOracleID,
						oracle:   uuidString(r.ScryfallOracleID),
						name:     r.Name,
						identity: c.identity,
					})
				}
				targets = append(targets, t)
			}
		}
		if last := lastCommander[c.oracle]; isStale(last) {
			targets = append(targets, target{identity: c.identity, commander: c, lastFetched: last})
		}
	}

	sort.SliceStable(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if !a.lastFetched.Equal(b.lastFetched) {
			return a.lastFetched.Before(b.lastFetched) // zero (never) sorts first
		}
		return a.baseline && !b.baseline
	})
	return targets, unresolved, nil
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
// first, then fetches and tallies each into s (SYN-010). A deck that vanished
// between search and fetch is skipped (SYN-005).
//
// @spec SYN-010, SYN-005
func sampleCommanderDecks(ctx context.Context, c *archidektClient, cmd commanderRef, want, pages int, s *sample) error {
	searchName := frontFace(cmd.name)
	var ids []int64
	for page := 1; page <= pages && len(ids) < want; page++ {
		sp, err := c.search(ctx, searchName, page)
		if err != nil {
			return err
		}
		for _, r := range sp.Results {
			if isCandidate(r) {
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
	for i := 0; i+4 <= len(name); i++ {
		if name[i:i+4] == " // " {
			return name[:i]
		}
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
