package cardsync_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"manafold-backend/internal/cardsync"
	db "manafold-backend/internal/db/generated"
)

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// TestRun_IngestsFixture_DerivesFields runs the ingestion against the checked-in
// fixture files and asserts the derived fields land and the sync_runs rows
// record a successful terminal state. A real Scryfall run is acceptable to
// demonstrate the job but is not required in CI.
//
// @spec CARD-001, CARD-005, CARD-007
func TestRun_IngestsFixture_DerivesFields(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	res, err := cardsync.Run(ctx, pool, cardsync.Options{
		OracleCardsPath:  "testdata/oracle_cards.json",
		DefaultCardsPath: "testdata/default_cards.json",
	})
	if err != nil {
		t.Fatalf("cardsync.Run: %v", err)
	}
	if res.OracleUpserted != 4 {
		t.Errorf("OracleUpserted = %d, want 4", res.OracleUpserted)
	}
	if res.PrintsUpserted != 2 {
		t.Errorf("PrintsUpserted = %d, want 2", res.PrintsUpserted)
	}
	if res.PrintsSkipped != 1 {
		t.Errorf("PrintsSkipped = %d, want 1 (the orphan printing, CARD-005)", res.PrintsSkipped)
	}

	q := db.New(pool)

	unlimited, err := q.GetCardByScryfallOracleID(ctx, mustUUID(t, "22222222-2222-2222-2222-222222222222"))
	if err != nil {
		t.Fatalf("load rat swarm: %v", err)
	}
	if !unlimited.SingletonLimit.Valid || unlimited.SingletonLimit.Int32 != 0 {
		t.Errorf("rat swarm singleton_limit = %+v, want {0, valid}", unlimited.SingletonLimit)
	}

	capped, err := q.GetCardByScryfallOracleID(ctx, mustUUID(t, "33333333-3333-3333-3333-333333333333"))
	if err != nil {
		t.Fatalf("load septet: %v", err)
	}
	if !capped.SingletonLimit.Valid || capped.SingletonLimit.Int32 != 7 {
		t.Errorf("septet singleton_limit = %+v, want {7, valid}", capped.SingletonLimit)
	}

	boss, err := q.GetCardByScryfallOracleID(ctx, mustUUID(t, "11111111-1111-1111-1111-111111111111"))
	if err != nil {
		t.Fatalf("load goblin boss: %v", err)
	}
	if !boss.CanBeCommander {
		t.Error("legendary creature can_be_commander = false, want true")
	}
	if boss.SingletonLimit.Valid {
		t.Errorf("goblin boss singleton_limit = %+v, want NULL", boss.SingletonLimit)
	}
	if len(boss.ColorIdentity) != 1 || boss.ColorIdentity[0] != "R" {
		t.Errorf("color_identity = %v, want [R] (stored verbatim, CARD-002)", boss.ColorIdentity)
	}

	walker, err := q.GetCardByScryfallOracleID(ctx, mustUUID(t, "44444444-4444-4444-4444-444444444444"))
	if err != nil {
		t.Fatalf("load walker: %v", err)
	}
	if !walker.CanBeCommander {
		t.Error("planeswalker with \"can be your commander\" text can_be_commander = false, want true")
	}

	oracleRun, err := q.LatestSyncRun(ctx, "oracle_cards")
	if err != nil {
		t.Fatalf("latest oracle_cards sync run: %v", err)
	}
	if oracleRun.Status != "succeeded" || oracleRun.RowsUpserted != 4 {
		t.Errorf("oracle_cards sync run = {status %q, rows %d}, want {succeeded, 4}", oracleRun.Status, oracleRun.RowsUpserted)
	}

	printsRun, err := q.LatestSyncRun(ctx, "default_cards")
	if err != nil {
		t.Fatalf("latest default_cards sync run: %v", err)
	}
	if printsRun.Status != "succeeded" {
		t.Errorf("default_cards sync run status = %q, want succeeded", printsRun.Status)
	}
}
