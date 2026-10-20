package synergy

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

// State is a commander's synergy status (SYN-040).
type State string

const (
	StateNone      State = "none"      // no fresh sample and nothing queued
	StateGathering State = "gathering" // a job is queued or running
	StateReady     State = "ready"     // a sample fresher than the TTL is stored
)

// Why a commander is `none` for a cause the caller can act on (SYN-001, SYN-032).
const (
	ReasonDisabled      = "disabled"
	ReasonDailyCap      = "daily_cap"
	ReasonNotACommander = "not_a_commander"
)

// dailyWindow is the rolling window of the new-commander cap (SYN-032);
// capLockKey is the advisory lock that serialises the cap check and insert.
const (
	dailyWindow       = 24 * time.Hour
	capLockKey  int64 = 0x4d46535943
)

// ErrNoCard means the card id names no card in the mirror (SYN-042).
var ErrNoCard = errors.New("no such card")

// Options configures a Service. Zero values take the defaults in withDefaults.
type Options struct {
	// Enabled turns on queuing and the worker (SYN-001). Off by default.
	Enabled bool

	// BaseURL replaces https://archidekt.com (tests point it at a fixture
	// server). HTTPClient replaces the default client.
	BaseURL    string
	HTTPClient *http.Client

	DecksPerCommander         int           // default 40
	SearchPages               int           // default 3
	BaselineCommanders        int           // default 5
	DecksPerBaselineCommander int           // default 8
	TTL                       time.Duration // default 14 days
	MaxNewPerDay              int           // default 10
	FailureCooldown           time.Duration // default 15 minutes

	// Logf receives progress lines; nil means log.Printf.
	Logf func(format string, args ...any)
}

func (o Options) withDefaults() Options {
	if o.DecksPerCommander <= 0 {
		o.DecksPerCommander = 40
	}
	if o.SearchPages <= 0 {
		o.SearchPages = 3
	}
	if o.BaselineCommanders <= 0 {
		o.BaselineCommanders = 5
	}
	if o.DecksPerBaselineCommander <= 0 {
		o.DecksPerBaselineCommander = 8
	}
	if o.TTL <= 0 {
		o.TTL = 14 * 24 * time.Hour
	}
	if o.MaxNewPerDay <= 0 {
		o.MaxNewPerDay = 10
	}
	if o.FailureCooldown <= 0 {
		o.FailureCooldown = 15 * time.Minute
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	return o
}

// OptionsFromEnv reads the SYNERGY_* environment (see backend/.env.example).
// Every variable is optional; unset or invalid values take the defaults.
func OptionsFromEnv() Options {
	posInt := func(key string) int {
		n, err := strconv.Atoi(os.Getenv(key))
		if err != nil || n <= 0 {
			return 0
		}
		return n
	}
	return Options{
		Enabled:                   strings.EqualFold(os.Getenv("SYNERGY_ENABLED"), "true"),
		DecksPerCommander:         posInt("SYNERGY_DECKS_PER_COMMANDER"),
		SearchPages:               posInt("SYNERGY_SEARCH_PAGES"),
		BaselineCommanders:        posInt("SYNERGY_BASELINE_COMMANDERS"),
		DecksPerBaselineCommander: posInt("SYNERGY_DECKS_PER_BASELINE_COMMANDER"),
		TTL:                       time.Duration(posInt("SYNERGY_TTL_DAYS")) * 24 * time.Hour,
		MaxNewPerDay:              posInt("SYNERGY_MAX_NEW_COMMANDERS_PER_DAY"),
		FailureCooldown:           time.Duration(posInt("SYNERGY_FAILURE_COOLDOWN_MINUTES")) * time.Minute,
	}
}

// Service queues, gathers, and reports on per-commander synergy. The API calls
// Ensure and Commander; one API process runs RunWorker; cmd/synergysync calls
// Enqueue and Drain.
type Service struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	opts   Options
	client *archidektClient
	wake   chan struct{}
}

func NewService(pool *pgxpool.Pool, opts Options) *Service {
	opts = opts.withDefaults()
	return &Service{
		pool:   pool,
		q:      db.New(pool),
		opts:   opts,
		client: newArchidektClient(opts.BaseURL, opts.HTTPClient, 0),
		wake:   make(chan struct{}, 1),
	}
}

// Enabled reports whether gathering is switched on (SYN-001).
func (s *Service) Enabled() bool { return s.opts.Enabled }

// CommanderView is a commander's status and, when any sample is stored, its
// sample and top cards.
type CommanderView struct {
	Card   db.GetSynergyCommanderCardRow
	State  State
	Reason string // set only when State is StateNone
	Sample *db.GetSynergySampleRow
	Cards  []db.ListTopSynergyCardsRow
}

// Status reports a commander's state from the local database (SYN-040): ready
// when its sample is fresher than the TTL, gathering when a job is queued or
// running, else none. A card id the mirror does not know is ErrNoCard.
//
// @spec SYN-040, SYN-042
func (s *Service) Status(ctx context.Context, commanderCardID pgtype.UUID) (CommanderView, error) {
	card, err := s.q.GetSynergyCommanderCard(ctx, commanderCardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommanderView{}, ErrNoCard
	}
	if err != nil {
		return CommanderView{}, err
	}
	v := CommanderView{Card: card, State: StateNone}

	sample, err := s.q.GetSynergySample(ctx, commanderCardID)
	switch {
	case err == nil:
		v.Sample = &sample
	case !errors.Is(err, pgx.ErrNoRows):
		return CommanderView{}, err
	}

	if v.Sample != nil && time.Since(v.Sample.FetchedAt.Time) < s.opts.TTL {
		v.State = StateReady
		return v, nil
	}
	active, err := s.q.HasActiveSynergyJob(ctx, card.ScryfallOracleID)
	if err != nil {
		return CommanderView{}, err
	}
	switch {
	case active:
		v.State = StateGathering
	case !card.Gatherable:
		v.Reason = ReasonNotACommander
	case !s.opts.Enabled:
		v.Reason = ReasonDisabled
	}
	return v, nil
}

// Commander is Status plus, when any sample is stored, its top cards — mirror
// rows only, commander-legal with banlist overrides applied — ordered by
// synergy. limit is clamped to [1, MaxTopCards], 0 meaning DefaultTopCards.
// It never calls Archidekt (SYN-040, SYN-041).
//
// @spec SYN-040, SYN-041
func (s *Service) Commander(ctx context.Context, commanderCardID pgtype.UUID, limit int) (CommanderView, error) {
	v, err := s.Status(ctx, commanderCardID)
	if err != nil || v.Sample == nil {
		return v, err
	}
	cards, err := s.q.ListTopSynergyCards(ctx, db.ListTopSynergyCardsParams{
		CommanderOracleID: v.Sample.CommanderOracleID,
		Lim:               int32(clampLimit(limit)),
	})
	if err != nil {
		return CommanderView{}, err
	}
	v.Cards = cards
	return v, nil
}

// Ensure is the trigger: it queues a gathering job for the commander when it
// has no fresh sample and no active job, subject to the daily cap, and returns
// its status. It reads and writes only Manafold's database; it never waits on
// Archidekt (SYN-030, SYN-031, SYN-032).
//
// @spec SYN-030, SYN-031, SYN-032
func (s *Service) Ensure(ctx context.Context, commanderCardID pgtype.UUID) (CommanderView, error) {
	v, err := s.Status(ctx, commanderCardID)
	if err != nil || v.State != StateNone || v.Reason != "" {
		return v, err
	}
	queued, capped, err := s.enqueue(ctx, v.Card.ScryfallOracleID, true)
	if err != nil {
		return CommanderView{}, err
	}
	switch {
	case capped:
		v.Reason = ReasonDailyCap
	case queued:
		v.State = StateGathering
		s.signal()
	default: // another request queued it first
		v.State = StateGathering
	}
	return v, nil
}

// Enqueue queues a job for a commander by oracle id, for the CLI: it skips the
// daily cap and the freshness check. queued is false when a job for the
// commander is already active (SYN-031, SYN-037).
//
// @spec SYN-037
func (s *Service) Enqueue(ctx context.Context, commanderOracleID pgtype.UUID) (queued bool, err error) {
	queued, _, err = s.enqueue(ctx, commanderOracleID, false)
	if queued {
		s.signal()
	}
	return queued, err
}

// enqueue inserts a queued job. With applyCap it first counts the jobs
// requested in the last 24 hours under an advisory transaction lock, so
// concurrent requests cannot overshoot the cap.
func (s *Service) enqueue(ctx context.Context, oracle pgtype.UUID, applyCap bool) (queued, capped bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)

	if applyCap {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, capLockKey); err != nil {
			return false, false, err
		}
		n, err := q.CountSynergyJobsSince(ctx, pgtype.Timestamptz{Time: time.Now().Add(-dailyWindow), Valid: true})
		if err != nil {
			return false, false, err
		}
		if int(n) >= s.opts.MaxNewPerDay {
			return false, true, nil
		}
	}
	if _, err := q.EnqueueSynergyJob(ctx, oracle); errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil // already queued or running
	} else if err != nil {
		return false, false, err
	}
	return true, false, tx.Commit(ctx)
}

// signal wakes an idle worker without blocking.
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// ResolveCommander finds a commander by name for the CLI: a case-insensitive
// exact name, or the front face of a multi-face name, that can be a commander
// (SYN-037). found is false when nothing matches.
//
// @spec SYN-037
func (s *Service) ResolveCommander(ctx context.Context, name string) (oracle pgtype.UUID, cardName string, found bool, err error) {
	row, err := s.q.ResolveSynergyCommander(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, "", false, nil
	}
	if err != nil {
		return pgtype.UUID{}, "", false, err
	}
	return row.ScryfallOracleID, row.Name, true, nil
}
