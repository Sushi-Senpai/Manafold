package synergy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	db "manafold-backend/internal/db/generated"
)

const (
	testCommanderName = "Synergy Test Commander"
	testIdentity      = "WG"
	baselineOracle    = "aaaaaaaa-0000-4000-8000-000000000100"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, ok := parseUUID(s)
	if !ok {
		t.Fatalf("bad uuid %q", s)
	}
	return u
}

func upsertTestCard(t *testing.T, q *db.Queries, oracle, name string, identity []string, commander bool, rank int32, commanderLegal string) pgtype.UUID {
	t.Helper()
	p := db.UpsertCardParams{
		ScryfallOracleID:       mustUUID(t, oracle),
		Name:                   name,
		ManaValue:              2,
		TypeLine:               "Creature",
		Colors:                 identity,
		ColorIdentity:          identity,
		ProducedMana:           []string{},
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"` + commanderLegal + `"}`),
		Layout:                 "normal",
		CanBeCommander:         commander,
		CommanderColorIdentity: identity,
	}
	if commander {
		p.EdhrecRank = pgtype.Int4{Int32: rank, Valid: true}
	}
	c, err := q.UpsertCard(context.Background(), p)
	if err != nil {
		t.Fatalf("upsert card %q: %v", name, err)
	}
	return c.ID
}

type mirrorIDs struct {
	commander, baseline, solRing pgtype.UUID
}

// seedSynergyMirror puts the test commander, a baseline commander (rank 0, so
// it outranks every real card), and the fixture decks' cards into the card
// mirror, and clears any synergy rows and jobs a previous run left for them.
// Skullclamp is deliberately left out of the mirror, Impact Tremors is not
// commander-legal, and Goblin Chieftain is banned by an override.
func seedSynergyMirror(t *testing.T, pool *pgxpool.Pool) mirrorIDs {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)

	ids := mirrorIDs{
		commander: upsertTestCard(t, q, krenko, testCommanderName, []string{"W", "G"}, true, 0, "legal"),
		baseline:  upsertTestCard(t, q, baselineOracle, baselineCommanderName, []string{"W", "G"}, true, 0, "legal"),
		solRing:   upsertTestCard(t, q, solRing, "Synergy Test Sol Ring", []string{}, false, 0, "legal"),
	}
	upsertTestCard(t, q, warchief, "Synergy Test Warchief", []string{"W"}, false, 0, "legal")
	upsertTestCard(t, q, chieftain, "Synergy Test Chieftain", []string{"W"}, false, 0, "legal")
	upsertTestCard(t, q, impactTremors, "Synergy Test Tremors", []string{"W"}, false, 0, "banned")

	reset := func() {
		for _, stmt := range []string{
			`DELETE FROM synergy_jobs WHERE commander_oracle_id IN ('` + krenko + `', '` + baselineOracle + `')`,
			`DELETE FROM synergy_commanders WHERE commander_oracle_id IN ('` + krenko + `', '` + baselineOracle + `')`,
			`DELETE FROM synergy_baselines WHERE color_identity = '` + testIdentity + `'`,
			`DELETE FROM banlist_overrides WHERE card_name = 'Synergy Test Chieftain'`,
		} {
			if _, err := pool.Exec(context.Background(), stmt); err != nil {
				t.Fatalf("reset: %v", err)
			}
		}
	}
	reset()
	t.Cleanup(reset)
	if _, err := pool.Exec(ctx, `INSERT INTO banlist_overrides (card_name, banned) VALUES ('Synergy Test Chieftain', true)`); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	return ids
}

// testOptions points a service at the fixture server with the baseline limited
// to the one seeded baseline commander, so results do not depend on what else
// the mirror holds, and with a daily cap no test run reaches.
func testOptions(baseURL string) Options {
	return Options{
		Enabled:            true,
		BaseURL:            baseURL,
		BaselineCommanders: 1,
		MaxNewPerDay:       100000,
		Logf:               func(string, ...any) {},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func ageSamples(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, stmt := range []string{
		`UPDATE synergy_commanders SET fetched_at = now() - interval '30 days' WHERE commander_oracle_id = '` + krenko + `'`,
		`UPDATE synergy_baselines SET fetched_at = now() - interval '30 days' WHERE color_identity = '` + testIdentity + `'`,
	} {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func jobCount(t *testing.T, pool *pgxpool.Pool, oracle string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM synergy_jobs WHERE commander_oracle_id = $1`, oracle).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// gather runs a forced job for the test commander to completion.
func gather(t *testing.T, svc *Service) DrainResult {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.Enqueue(ctx, mustUUID(t, krenko)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	res, err := svc.Drain(ctx)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	return res
}

// @spec SYN-030, SYN-031
func TestEnsure_QueuesOnceReportsGatheringAndNeverCallsArchidekt(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	svc := NewService(pool, testOptions(fs.URL))
	ctx := context.Background()

	v, err := svc.Ensure(ctx, ids.commander)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateGathering || v.Sample != nil {
		t.Fatalf("first ensure = %s/%v, want gathering with no sample", v.State, v.Sample)
	}
	v, err = svc.Ensure(ctx, ids.commander)
	if err != nil || v.State != StateGathering {
		t.Fatalf("second ensure = %s, %v, want gathering", v.State, err)
	}
	if n := jobCount(t, pool, krenko); n != 1 {
		t.Errorf("%d jobs for the commander, want exactly 1", n)
	}
	if len(fs.hits) != 0 {
		t.Errorf("ensure made %d Archidekt requests, want none", len(fs.hits))
	}

	st, err := svc.Status(ctx, ids.commander)
	if err != nil || st.State != StateGathering {
		t.Errorf("status = %s, %v, want gathering", st.State, err)
	}
}

// @spec SYN-031
func TestSchema_AtMostOneActiveJobPerCommander(t *testing.T) {
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	ctx := context.Background()
	insert := `INSERT INTO synergy_jobs (commander_oracle_id, status) VALUES ('` + krenko + `', $1)`

	if _, err := pool.Exec(ctx, insert, "queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insert, "running"); err == nil {
		t.Error("a second active job for the same commander was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE synergy_jobs SET status = 'failed' WHERE commander_oracle_id = $1`, krenko); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insert, "queued"); err != nil {
		t.Errorf("a new job after the old one finished was rejected: %v", err)
	}
}

// @spec SYN-001
func TestEnsure_DisabledQueuesNothing(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	opts := testOptions(fs.URL)
	opts.Enabled = false
	svc := NewService(pool, opts)

	v, err := svc.Ensure(context.Background(), ids.commander)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateNone || v.Reason != ReasonDisabled {
		t.Errorf("ensure = %s/%q, want none/disabled", v.State, v.Reason)
	}
	if n := jobCount(t, pool, krenko); n != 0 {
		t.Errorf("%d jobs queued while disabled", n)
	}
	// The worker does nothing at all when disabled.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	svc.RunWorker(ctx)
	if len(fs.hits) != 0 {
		t.Errorf("disabled worker made %d Archidekt requests", len(fs.hits))
	}
}

// @spec SYN-030, SYN-042
func TestEnsure_NotACommanderAndUnknownCard(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	svc := NewService(pool, testOptions("http://127.0.0.1:1"))
	ctx := context.Background()

	v, err := svc.Ensure(ctx, ids.solRing)
	if err != nil || v.State != StateNone || v.Reason != ReasonNotACommander {
		t.Errorf("ensure of a non-commander = %s/%q, %v, want none/not_a_commander", v.State, v.Reason, err)
	}
	if n := jobCount(t, pool, solRing); n != 0 {
		t.Errorf("%d jobs queued for a non-commander", n)
	}

	unknown := mustUUID(t, "bbbbbbbb-0000-4000-8000-000000000001")
	if _, err := svc.Ensure(ctx, unknown); !errors.Is(err, ErrNoCard) {
		t.Errorf("ensure of an unknown card: err = %v, want ErrNoCard", err)
	}
	if _, err := svc.Status(ctx, unknown); !errors.Is(err, ErrNoCard) {
		t.Errorf("status of an unknown card: err = %v, want ErrNoCard", err)
	}
}

// @spec SYN-032
func TestEnsure_DailyCapBlocksNewCommanders(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	ctx := context.Background()

	// Room for exactly one more job than the database already holds today.
	var already int32
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM synergy_jobs WHERE requested_at > now() - interval '24 hours'`).Scan(&already); err != nil {
		t.Fatal(err)
	}
	opts := testOptions("http://127.0.0.1:1")
	opts.MaxNewPerDay = int(already) + 1
	svc := NewService(pool, opts)

	v, err := svc.Ensure(ctx, ids.commander)
	if err != nil || v.State != StateGathering {
		t.Fatalf("first ensure = %s, %v, want gathering", v.State, err)
	}
	v, err = svc.Ensure(ctx, ids.baseline)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateNone || v.Reason != ReasonDailyCap {
		t.Errorf("ensure over the cap = %s/%q, want none/daily_cap", v.State, v.Reason)
	}
	if n := jobCount(t, pool, baselineOracle); n != 0 {
		t.Errorf("%d jobs queued past the cap", n)
	}
	// The commander already queued stays gathering; the cap only blocks new ones.
	if v, _ = svc.Ensure(ctx, ids.commander); v.State != StateGathering {
		t.Errorf("already-queued commander = %s, want gathering", v.State)
	}
}

// @spec SYN-011, SYN-020, SYN-021, SYN-033, SYN-034, SYN-035, SYN-040, SYN-041
func TestDrain_GathersBaselineThenCommanderAndServesIt(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	svc := NewService(pool, testOptions(fs.URL))
	ctx := context.Background()

	if v, _ := svc.Ensure(ctx, ids.commander); v.State != StateGathering {
		t.Fatalf("ensure = %s, want gathering", v.State)
	}
	res, err := svc.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res.Jobs != 1 || res.Requests == 0 {
		t.Errorf("drain = %+v, want 1 job and some requests", res)
	}

	v, err := svc.Commander(ctx, ids.commander, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateReady || v.Sample == nil {
		t.Fatalf("after the job: state %s sample %v, want ready", v.State, v.Sample)
	}

	// The commander sample is decks 101 + 107 (identical → 1 duplicate), 106
	// and 109. The baseline is deck 109 alone. Skullclamp is not in the mirror,
	// Impact Tremors is not commander-legal, Chieftain is banned by override.
	s := v.Sample
	if s.DeckCount != 3 || s.DuplicatesSkipped != 1 || s.ColorIdentity != testIdentity {
		t.Errorf("sample = %+v, want 3 decks, 1 duplicate, identity %s", s, testIdentity)
	}
	if !s.BaselineDeckCount.Valid || s.BaselineDeckCount.Int32 != 1 {
		t.Errorf("baseline deck count = %+v, want 1", s.BaselineDeckCount)
	}
	var names []string
	for _, c := range v.Cards {
		names = append(names, c.Card.Name)
	}
	if got := strings.Join(names, "|"); got != "Synergy Test Warchief|Synergy Test Sol Ring" {
		t.Fatalf("top cards = %q, want the commander-legal mirror cards ordered by synergy", got)
	}
	w, sr := v.Cards[0], v.Cards[1]
	if !near(w.InclusionRate, 2.0/3) || !w.BaselineRate.Valid || !near(w.BaselineRate.Float64, 0) || !near(w.Synergy.Float64, 2.0/3) {
		t.Errorf("warchief inclusion=%v baseline=%+v synergy=%+v", w.InclusionRate, w.BaselineRate, w.Synergy)
	}
	if !near(sr.InclusionRate, 1) || !near(sr.BaselineRate.Float64, 1) || !near(sr.Synergy.Float64, 0) {
		t.Errorf("sol ring inclusion=%v baseline=%+v synergy=%+v", sr.InclusionRate, sr.BaselineRate, sr.Synergy)
	}
	if limited, _ := svc.Commander(ctx, ids.commander, 1); len(limited.Cards) != 1 {
		t.Errorf("limit 1 returned %d cards", len(limited.Cards))
	}

	var status string
	var made int32
	if err := pool.QueryRow(ctx, `SELECT status, requests_made FROM synergy_jobs WHERE commander_oracle_id = $1`, krenko).Scan(&status, &made); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || int(made) != res.Requests {
		t.Errorf("job row = %s/%d requests, want succeeded/%d", status, made, res.Requests)
	}

	// Ready now: ensure queues nothing and calls nothing.
	hits := len(fs.hits)
	v2, err := svc.Ensure(ctx, ids.commander)
	if err != nil || v2.State != StateReady {
		t.Fatalf("ensure of a ready commander = %s, %v", v2.State, err)
	}
	if n := jobCount(t, pool, krenko); n != 1 || len(fs.hits) != hits {
		t.Errorf("ready ensure: %d jobs, %d new requests, want 1 job and none", n, len(fs.hits)-hits)
	}
}

// @spec SYN-030, SYN-040
func TestStatus_StaleSampleIsServedWhileRefreshing(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	svc := NewService(pool, testOptions(fs.URL))
	ctx := context.Background()
	gather(t, svc)
	ageSamples(t, pool)

	v, err := svc.Status(ctx, ids.commander)
	if err != nil || v.State != StateNone || v.Sample == nil {
		t.Fatalf("stale status = %s sample %v, %v, want none with the old sample", v.State, v.Sample, err)
	}
	v, err = svc.Ensure(ctx, ids.commander)
	if err != nil || v.State != StateGathering {
		t.Fatalf("ensure of a stale commander = %s, %v, want gathering", v.State, err)
	}
	v, err = svc.Commander(ctx, ids.commander, 0)
	if err != nil || v.State != StateGathering || len(v.Cards) == 0 {
		t.Errorf("while refreshing: %s with %d cards, %v, want gathering with the old cards", v.State, len(v.Cards), err)
	}
}

// @spec SYN-040, SYN-042
func TestStatus_CommanderWithNoSampleHasEmptyCards(t *testing.T) {
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	svc := NewService(pool, testOptions("http://127.0.0.1:1"))

	v, err := svc.Commander(context.Background(), ids.commander, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateNone || v.Sample != nil || len(v.Cards) != 0 {
		t.Errorf("view = %s sample %v cards %d, want none, no sample, no cards", v.State, v.Sample, len(v.Cards))
	}
}

// @spec SYN-034
func TestDrain_FreshBaselineIsNotResampled(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	svc := NewService(pool, testOptions(fs.URL))
	gather(t, svc)

	// Re-gather the commander while its baseline is still fresh.
	if _, err := pool.Exec(context.Background(), `UPDATE synergy_commanders SET fetched_at = now() - interval '30 days' WHERE commander_oracle_id = $1`, krenko); err != nil {
		t.Fatal(err)
	}
	before := len(fs.hits)
	gather(t, svc)
	for _, h := range fs.hits[before:] {
		if strings.Contains(h, "Baseline") {
			t.Errorf("a fresh baseline was sampled again: %s", h)
		}
	}
	if len(fs.hits) == before {
		t.Error("the second job made no requests at all")
	}
}

// @spec SYN-005, SYN-022, SYN-035
func TestDrain_FailureKeepsPreviousAggregatesAndRecordsTheError(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	svc := NewService(pool, testOptions(fs.URL))
	ctx := context.Background()
	gather(t, svc)
	ageSamples(t, pool)

	fs.failDecks = true
	if _, err := svc.Enqueue(ctx, mustUUID(t, krenko)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Drain(ctx); err == nil {
		t.Fatal("Drain succeeded though every deck request failed")
	}

	v, err := svc.Commander(ctx, ids.commander, 0)
	if err != nil || v.Sample == nil || v.Sample.DeckCount != 3 || len(v.Cards) != 2 {
		t.Errorf("previous aggregates not intact: %+v, %v", v.Sample, err)
	}
	if v.State != StateNone {
		t.Errorf("state after a failed job = %s, want none", v.State)
	}
	var status string
	var msg *string
	if err := pool.QueryRow(ctx, `SELECT status, error FROM synergy_jobs WHERE commander_oracle_id = $1 ORDER BY requested_at DESC LIMIT 1`, krenko).Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || msg == nil || *msg == "" {
		t.Errorf("job row = %s/%v, want failed with error text", status, msg)
	}
}

// @spec SYN-036
func TestWorker_PausesForTheCooldownAfterAFailedJob(t *testing.T) {
	fc := useFakeClock(t)
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	fs.failDecks = true
	opts := testOptions(fs.URL)
	opts.FailureCooldown = 17 * time.Minute
	svc := NewService(pool, opts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stop the worker once it sleeps for the cooldown.
	inner := sleepCtx
	sleepCtx = func(c context.Context, d time.Duration) error {
		err := inner(c, d)
		if d == opts.FailureCooldown {
			cancel()
		}
		return err
	}
	t.Cleanup(func() { sleepCtx = inner })

	if _, err := svc.Enqueue(ctx, mustUUID(t, krenko)); err != nil {
		t.Fatal(err)
	}
	err := svc.withWorkerLock(ctx, func(conn *pgxpool.Conn) error {
		return svc.processQueue(ctx, false, conn, &DrainResult{})
	})
	if err != nil {
		t.Fatal(err)
	}
	var cooled bool
	for _, d := range fc.sleeps {
		if d == opts.FailureCooldown {
			cooled = true
		}
	}
	if !cooled {
		t.Errorf("worker never paused for the %v cooldown; sleeps = %v", opts.FailureCooldown, fc.sleeps)
	}
}

// @spec SYN-034
func TestDrain_JobFailsWhenItExceedsItsRequestCap(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	fs.tooManyFirst = 3 // every URL answers 429 three times before it answers
	opts := testOptions(fs.URL)
	opts.SearchPages = 1
	opts.DecksPerCommander = 2
	opts.DecksPerBaselineCommander = 2
	svc := NewService(pool, opts)
	ctx := context.Background()

	if _, err := svc.Enqueue(ctx, mustUUID(t, krenko)); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Drain(ctx)
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("Drain err = %v, want the request-cap failure", err)
	}
	// Worst case is 3 (baseline) + 3 (commander) + the retry allowance.
	if want := 3 + 3 + retryAllowance; res.Requests != want {
		t.Errorf("requests = %d, want exactly the cap %d", res.Requests, want)
	}
	// The commander's sample never completed, so it was not written.
	if v, _ := svc.Status(ctx, ids.commander); v.Sample != nil {
		t.Errorf("a commander sample was stored though its job exceeded the cap: %+v", v.Sample)
	}
}

// @spec SYN-035
func TestDrain_RequeuesJobsLeftRunningByADeadProcess(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	svc := NewService(pool, testOptions(fs.URL))
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO synergy_jobs (commander_oracle_id, status, started_at) VALUES ($1, 'running', now())`, krenko); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Drain(ctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if v, _ := svc.Status(ctx, ids.commander); v.State != StateReady {
		t.Errorf("state = %s, want ready: the orphaned job should have been requeued and run", v.State)
	}
}

// @spec SYN-033, SYN-037
func TestDrain_RefusesWhileAnotherProcessHoldsTheWorkerLock(t *testing.T) {
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	svc := NewService(pool, testOptions("http://127.0.0.1:1"))
	ctx := context.Background()

	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	var got bool
	if err := holder.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, workerLockKey).Scan(&got); err != nil || !got {
		t.Fatalf("could not take the lock: %v %v", got, err)
	}
	defer holder.Exec(ctx, `SELECT pg_advisory_unlock($1)`, workerLockKey)

	if _, err := svc.Drain(ctx); !errors.Is(err, ErrWorkerBusy) {
		t.Errorf("Drain err = %v, want ErrWorkerBusy", err)
	}
}

// @spec SYN-011
func TestPlanJob_BaselineExcludesTheCommanderAndSampledCommanders(t *testing.T) {
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	svc := NewService(pool, testOptions("http://127.0.0.1:1"))
	ctx := context.Background()
	cmd := commanderRef{oracleID: mustUUID(t, krenko), oracle: krenko, name: testCommanderName, identity: testIdentity}

	targets, err := svc.planJob(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || !targets[0].baseline || targets[1].baseline {
		t.Fatalf("targets = %+v, want the baseline then the commander", targets)
	}
	if got := targets[0].sources; len(got) != 1 || got[0].name != baselineCommanderName {
		t.Errorf("baseline sources = %+v, want only the rank-0 baseline commander (never the commander itself)", got)
	}

	// Once the baseline commander has its own sample it is no longer a source.
	empty := newSample()
	bc := target{identity: testIdentity, commander: commanderRef{oracleID: mustUUID(t, baselineOracle), oracle: baselineOracle, name: baselineCommanderName, identity: testIdentity}}
	if err := writeTarget(ctx, pool, bc, empty, time.Now()); err != nil {
		t.Fatal(err)
	}
	targets, err = svc.planJob(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range targets[0].sources {
		if src.name == baselineCommanderName {
			t.Errorf("a commander with a stored sample is still a baseline source")
		}
	}
}

// @spec SYN-037
func TestResolveCommander_ExactAndFrontFaceNames(t *testing.T) {
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	svc := NewService(pool, testOptions("http://127.0.0.1:1"))
	ctx := context.Background()

	oracle, name, found, err := svc.ResolveCommander(ctx, "synergy TEST commander")
	if err != nil || !found || name != testCommanderName || uuidString(oracle) != krenko {
		t.Errorf("resolve = %v %q %v %v", uuidString(oracle), name, found, err)
	}
	if _, _, found, _ := svc.ResolveCommander(ctx, "Synergy Test Sol Ring"); found {
		t.Error("a card that cannot be a commander resolved")
	}
	if _, _, found, _ := svc.ResolveCommander(ctx, "Synergy Test No Such Commander"); found {
		t.Error("an unknown name resolved")
	}
}

// @spec SYN-020
func TestSchema_StoresAggregatesOnly(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(context.Background(), `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name LIKE 'synergy\_%' AND table_name <> 'synergy_jobs'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"deck_id", "deck_name", "owner", "user", "url", "decklist"} {
			if strings.Contains(col, banned) {
				t.Errorf("%s.%s looks like raw deck or user data", table, col)
			}
		}
	}
}

// @spec SYN-021, SYN-022
func TestWriteTarget_SynergyIsNullUntilTheBaselineLandsThenRecomputed(t *testing.T) {
	pool := testPool(t)
	ids := seedSynergyMirror(t, pool)
	svc := NewService(pool, testOptions("http://127.0.0.1:1"))
	ctx := context.Background()

	cmdSample := newSample()
	cmdSample.add([]string{solRing, warchief})
	cmdSample.add([]string{solRing, chieftain, warchief})
	cmd := target{identity: testIdentity, commander: commanderRef{oracleID: mustUUID(t, krenko), oracle: krenko, name: testCommanderName, identity: testIdentity}}
	if err := writeTarget(ctx, pool, cmd, cmdSample, time.Now()); err != nil {
		t.Fatalf("write commander: %v", err)
	}

	v, err := svc.Commander(ctx, ids.commander, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Cards) == 0 {
		t.Fatal("no cards stored for the commander")
	}
	for _, c := range v.Cards {
		if c.BaselineRate.Valid || c.Synergy.Valid {
			t.Errorf("%s has baseline %+v synergy %+v, want NULL before any baseline exists", c.Card.Name, c.BaselineRate, c.Synergy)
		}
	}

	// The baseline lands afterwards: synergy is recomputed for the commander.
	// Sol Ring is in every baseline deck; Warchief is absent from a non-empty
	// baseline and counts as rate 0.
	baseSample := newSample()
	baseSample.add([]string{solRing, skullclamp})
	baseline := target{baseline: true, identity: testIdentity, sources: []commanderRef{{name: baselineCommanderName}}}
	if err := writeTarget(ctx, pool, baseline, baseSample, time.Now()); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	v, err = svc.Commander(ctx, ids.commander, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, c := range v.Cards {
		if !c.Synergy.Valid {
			t.Errorf("%s synergy still NULL after the baseline landed", c.Card.Name)
		}
		got[c.Card.Name] = c.Synergy.Float64
	}
	if !near(got["Synergy Test Warchief"], 1) || !near(got["Synergy Test Sol Ring"], 0) {
		t.Errorf("synergy = %v, want Warchief 1 and Sol Ring 0", got)
	}
}
