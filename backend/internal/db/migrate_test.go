package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// TestMigrate_FromEmptySchema_ReachesLatestVersion runs Migrate against a
// brand-new, empty Postgres schema — not CI's own `migrate ... up` step, which
// by the time `go test` runs has already brought the default `public` schema
// current and would let this pass even if Migrate itself were broken.
// Exercising Migrate from nothing is what would catch a bug in the embedded
// migration path before it reached production.
//
// @spec PLATFORM-001
func TestMigrate_FromEmptySchema_ReachesLatestVersion(t *testing.T) {
	_ = godotenv.Load("../../.env")
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}

	ctx := context.Background()
	schema := "migrate_test_" + randomHex(t)
	schemaURL := withSearchPath(baseURL, schema)

	adminPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+quoteIdent(schema)); err != nil {
		t.Fatalf("create isolated test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA `+quoteIdent(schema)+` CASCADE`); err != nil {
			t.Logf("cleanup: drop test schema %s: %v", schema, err)
		}
	})

	if err := Migrate(schemaURL); err != nil {
		t.Fatalf("Migrate against an empty schema: %v", err)
	}

	scopedPool, err := pgxpool.New(ctx, schemaURL)
	if err != nil {
		t.Fatalf("connect with the migrated schema: %v", err)
	}
	defer scopedPool.Close()

	// The M1 tables must be queryable after Migrate.
	for _, probe := range []string{
		"SELECT id, name, color_identity, singleton_limit, can_be_commander FROM cards LIMIT 0",
		"SELECT id, scryfall_id, card_id FROM card_prints LIMIT 0",
		"SELECT id, bulk_type, status FROM sync_runs LIMIT 0",
		"SELECT id, card_name, banned FROM banlist_overrides LIMIT 0", // @spec CARD-030
		"SELECT id, email FROM users LIMIT 0",
		"SELECT id, user_id, color_identity, is_public FROM decks LIMIT 0",
		"SELECT id, deck_id, card_id, board, quantity FROM deck_cards LIMIT 0",
	} {
		if _, err := scopedPool.Exec(ctx, probe); err != nil {
			t.Errorf("probe %q failed after Migrate: %v", probe, err)
		}
	}

	// A second Migrate against an already-current schema must no-op, not error —
	// the path every redeploy without a new migration takes.
	if err := Migrate(schemaURL); err != nil {
		t.Errorf("Migrate against an already-current schema: %v", err)
	}
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate random schema suffix: %v", err)
	}
	return hex.EncodeToString(b)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// withSearchPath appends a search_path parameter so a pool built from the URL
// resolves unqualified names inside the isolated test schema.
func withSearchPath(baseURL, schema string) string {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	return baseURL + sep + "search_path=" + schema
}
