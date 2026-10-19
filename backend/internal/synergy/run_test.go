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

// seedSynergyMirror puts the test commander, a baseline commander, and the
// fixture decks' cards into the card mirror, and clears any synergy rows a
// previous run left for the test commander and identity. Skullclamp is
// deliberately left out of the mirror, Impact Tremors is not commander-legal,
// and Goblin Chieftain is banned by an override.
func seedSynergyMirror(t *testing.T, pool *pgxpool.Pool) (commanderID, baselineID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)

	commanderID = upsertTestCard(t, q, krenko, testCommanderName, []string{"W", "G"}, true, 0, "legal")
	baselineID = upsertTestCard(t, q, baselineOracle, baselineCommanderName, []string{"W", "G"}, true, 0, "legal")
	upsertTestCard(t, q, solRing, "Synergy Test Sol Ring", []string{}, false, 0, "legal")
	upsertTestCard(t, q, warchief, "Synergy Test Warchief", []string{"W"}, false, 0, "legal")
	upsertTestCard(t, q, chieftain, "Synergy Test Chieftain", []string{"W"}, false, 0, "legal")
	upsertTestCard(t, q, impactTremors, "Synergy Test Tremors", []string{"W"}, false, 0, "banned")

	reset := func() {
		for _, stmt := range []string{
			`DELETE FROM synergy_commanders WHERE commander_oracle_id = '` + krenko + `'`,
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
	return commanderID, baselineID
}

func quietOptions(baseURL string) Options {
	return Options{
		BaseURL:    baseURL,
		Commanders: []string{testCommanderName, "Synergy Test No Such Commander"},
		Logf:       func(string, ...any) {},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// @spec SYN-001, SYN-011, SYN-020, SYN-021, SYN-022, SYN-031, SYN-032, SYN-040, SYN-041, SYN-042
func TestRun_SamplesBaselineAndCommander_StoresSynergyAndResumes(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	commanderID, baselineID := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	ctx := context.Background()

	res, err := Run(ctx, pool, quietOptions(fs.URL))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// One baseline (WG) and one commander target. The unresolvable name is
	// reported and skipped, not fatal.
	if res.TargetsRefreshed != 2 || res.TargetsPending != 0 {
		t.Errorf("refreshed=%d pending=%d, want 2 and 0", res.TargetsRefreshed, res.TargetsPending)
	}
	if len(res.Unresolved) != 1 || res.Unresolved[0] != "Synergy Test No Such Commander" {
		t.Errorf("unresolved = %v", res.Unresolved)
	}
	if res.RequestsMade == 0 {
		t.Error("run made no requests")
	}

	q := db.New(pool)
	top, err := TopCards(ctx, q, commanderID, 0)
	if err != nil {
		t.Fatalf("TopCards: %v", err)
	}

	// The commander sample is decks 101 + 107 (identical → 1 duplicate), 106
	// and 109. The baseline is deck 109 alone. Skullclamp is not in the mirror,
	// Impact Tremors is not commander-legal, Chieftain is banned by override.
	s := top.Sample
	if s.DeckCount != 3 || s.DuplicatesSkipped != 1 || s.ColorIdentity != testIdentity {
		t.Errorf("sample = %+v, want 3 decks, 1 duplicate, identity %s", s, testIdentity)
	}
	if !s.BaselineDeckCount.Valid || s.BaselineDeckCount.Int32 != 1 {
		t.Errorf("baseline deck count = %+v, want 1", s.BaselineDeckCount)
	}
	var names []string
	for _, c := range top.Cards {
		names = append(names, c.Card.Name)
	}
	if got := strings.Join(names, "|"); got != "Synergy Test Warchief|Synergy Test Sol Ring" {
		t.Fatalf("top cards = %q, want the commander-legal mirror cards ordered by synergy", got)
	}
	warchiefRow, solRow := top.Cards[0], top.Cards[1]
	if !near(warchiefRow.InclusionRate, 2.0/3) || !warchiefRow.BaselineRate.Valid || !near(warchiefRow.BaselineRate.Float64, 0) || !near(warchiefRow.Synergy.Float64, 2.0/3) {
		t.Errorf("warchief inclusion=%v baseline=%+v synergy=%+v", warchiefRow.InclusionRate, warchiefRow.BaselineRate, warchiefRow.Synergy)
	}
	if !near(solRow.InclusionRate, 1) || !near(solRow.BaselineRate.Float64, 1) || !near(solRow.Synergy.Float64, 0) {
		t.Errorf("sol ring inclusion=%v baseline=%+v synergy=%+v", solRow.InclusionRate, solRow.BaselineRate, solRow.Synergy)
	}

	if _, err := TopCards(ctx, q, baselineID, 0); !errors.Is(err, ErrNoSample) {
		t.Errorf("a commander with no sample: err = %v, want ErrNoSample", err)
	}
	if limited, _ := TopCards(ctx, q, commanderID, 1); len(limited.Cards) != 1 {
		t.Errorf("limit 1 returned %d cards", len(limited.Cards))
	}

	// Audit row.
	var status string
	var refreshed int32
	if err := pool.QueryRow(ctx, `SELECT status, targets_refreshed FROM synergy_runs ORDER BY started_at DESC LIMIT 1`).Scan(&status, &refreshed); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || refreshed != 2 {
		t.Errorf("synergy_runs row = %s/%d, want succeeded/2", status, refreshed)
	}

	// A second run inside the refresh window re-fetches nothing.
	before := len(fs.hits)
	res2, err := Run(ctx, pool, quietOptions(fs.URL))
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res2.TargetsRefreshed != 0 || res2.RequestsMade != 0 || len(fs.hits) != before {
		t.Errorf("second run refreshed=%d requests=%d new hits=%d, want nothing re-fetched",
			res2.TargetsRefreshed, res2.RequestsMade, len(fs.hits)-before)
	}
}

// @spec SYN-020
func TestSchema_StoresAggregatesOnly(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(context.Background(), `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name LIKE 'synergy\_%' AND table_name <> 'synergy_runs'`)
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

// @spec SYN-030
func TestRun_StartsATargetOnlyWhenTheBudgetCoversItsWorstCase(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)

	opts := quietOptions(fs.URL)
	opts.MaxRequests = 5 // below every target's worst case (3 pages + 60 decks)
	res, err := Run(context.Background(), pool, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.RequestsMade != 0 || res.TargetsRefreshed != 0 || res.TargetsPending != 2 {
		t.Errorf("requests=%d refreshed=%d pending=%d, want 0, 0, 2", res.RequestsMade, res.TargetsRefreshed, res.TargetsPending)
	}
	if len(fs.hits) != 0 {
		t.Errorf("server saw %d requests though no target fit the budget", len(fs.hits))
	}
	var status string
	var pending int32
	if err := pool.QueryRow(context.Background(), `SELECT status, targets_pending FROM synergy_runs ORDER BY started_at DESC LIMIT 1`).Scan(&status, &pending); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || pending != 2 {
		t.Errorf("synergy_runs row = %s/%d pending, want succeeded/2", status, pending)
	}
}

// @spec SYN-005, SYN-022, SYN-031, SYN-032
func TestRun_FailureLeavesPreviousAggregatesAndIsRecorded(t *testing.T) {
	useFakeClock(t)
	pool := testPool(t)
	commanderID, _ := seedSynergyMirror(t, pool)
	fs := newFixtureServer(t)
	ctx := context.Background()

	if _, err := Run(ctx, pool, quietOptions(fs.URL)); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Age both targets so the next run refreshes them, against a source that
	// now fails every deck detail.
	for _, stmt := range []string{
		`UPDATE synergy_commanders SET fetched_at = now() - interval '30 days' WHERE commander_oracle_id = '` + krenko + `'`,
		`UPDATE synergy_baselines SET fetched_at = now() - interval '30 days' WHERE color_identity = '` + testIdentity + `'`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	fs.failDecks = true
	res, err := Run(ctx, pool, quietOptions(fs.URL))
	if err == nil {
		t.Fatal("Run succeeded though every deck request failed")
	}
	if res.TargetsRefreshed != 0 || res.TargetsPending != 2 {
		t.Errorf("refreshed=%d pending=%d, want 0 and 2", res.TargetsRefreshed, res.TargetsPending)
	}

	top, err := TopCards(ctx, db.New(pool), commanderID, 0)
	if err != nil || top.Sample.DeckCount != 3 || len(top.Cards) != 2 {
		t.Errorf("previous aggregates not intact: %+v, %v", top.Sample, err)
	}
	var status string
	var msg *string
	if err := pool.QueryRow(ctx, `SELECT status, error FROM synergy_runs ORDER BY started_at DESC LIMIT 1`).Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || msg == nil || *msg == "" {
		t.Errorf("synergy_runs row = %s/%v, want failed with error text", status, msg)
	}
}

// @spec SYN-021, SYN-022
func TestWriteTarget_SynergyIsNullUntilTheBaselineLandsThenRecomputed(t *testing.T) {
	pool := testPool(t)
	commanderID, _ := seedSynergyMirror(t, pool)
	ctx := context.Background()

	cmdSample := newSample()
	cmdSample.add([]string{solRing, warchief})
	cmdSample.add([]string{solRing, chieftain, warchief})
	cmd := target{identity: testIdentity, commander: commanderRef{oracleID: mustUUID(t, krenko), oracle: krenko, name: testCommanderName, identity: testIdentity}}
	if err := writeTarget(ctx, pool, cmd, cmdSample, time.Now()); err != nil {
		t.Fatalf("write commander: %v", err)
	}

	q := db.New(pool)
	top, err := TopCards(ctx, q, commanderID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Cards) == 0 {
		t.Fatal("no cards stored for the commander")
	}
	for _, c := range top.Cards {
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
	top, err = TopCards(ctx, q, commanderID, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, c := range top.Cards {
		if !c.Synergy.Valid {
			t.Errorf("%s synergy still NULL after the baseline landed", c.Card.Name)
		}
		got[c.Card.Name] = c.Synergy.Float64
	}
	if !near(got["Synergy Test Warchief"], 1) || !near(got["Synergy Test Sol Ring"], 0) {
		t.Errorf("synergy = %v, want Warchief 1 and Sol Ring 0", got)
	}
}
