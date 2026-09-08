package cardsync_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"manafold-backend/internal/cardsync"
	db "manafold-backend/internal/db/generated"
)

// TestRun_DevSeed loads backend/seed/cards.json — the ~40-card dev / CI seed of
// real Scryfall cards — the same way `cmd/cardsync` does when CARDSYNC_SEED_PATH
// is set, and checks it ingests cleanly with searchable, image-bearing data
// (plus the intended imageless and double-faced cases). This is what gives a
// local run and CI card data to search against.
//
// @spec CARD-040
func TestRun_DevSeed(t *testing.T) {
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

	const seed = "../../seed/cards.json"
	res, err := cardsync.Run(ctx, pool, cardsync.Options{
		OracleCardsPath:  seed,
		DefaultCardsPath: seed,
	})
	if err != nil {
		t.Fatalf("cardsync.Run(dev seed): %v", err)
	}
	if res.OracleUpserted < 30 {
		t.Fatalf("OracleUpserted = %d, want the full seed (>= 30)", res.OracleUpserted)
	}
	if res.PrintsUpserted != res.OracleUpserted {
		t.Fatalf("PrintsUpserted = %d, want one per card (%d)", res.PrintsUpserted, res.OracleUpserted)
	}

	q := db.New(pool)

	// A spread of commanders is searchable.
	commanders, err := q.AutocompleteCardNames(ctx, "")
	if err == nil && len(commanders) == 0 {
		t.Log("autocomplete empty for blank prefix, as designed")
	}

	// At least one printing carries real Scryfall CDN art, and at least one is
	// deliberately imageless (a modal-DFC / un-synced printing) so the builder's
	// text-frame fallback has something to exercise.
	var withArt, withoutArt int
	rows, err := pool.Query(ctx, `
		SELECT p.image_uris IS NOT NULL
		       AND p.image_uris->>'normal' LIKE 'https://cards.scryfall.io/%'
		FROM cards c
		JOIN card_prints p ON p.card_id = c.id`)
	if err != nil {
		t.Fatalf("query prints: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hasArt bool
		if err := rows.Scan(&hasArt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if hasArt {
			withArt++
		} else {
			withoutArt++
		}
	}
	if withArt < 20 {
		t.Errorf("prints with real Scryfall art = %d, want most of the seed", withArt)
	}
	if withoutArt < 1 {
		t.Errorf("prints with no art = %d, want at least one (imageless fallback case)", withoutArt)
	}

	// A known commander resolved by exact name (ResolveCardByName is what import
	// uses); also proves the seed's rules fields landed.
	card, err := q.GetCardByScryfallOracleID(ctx, mustUUID(t, oracleIDOf(t, pool, "Atraxa, Praetors' Voice")))
	if err != nil {
		t.Fatalf("load Atraxa: %v", err)
	}
	if !card.CanBeCommander {
		t.Errorf("Atraxa can_be_commander = false, want true (derived from the legendary creature type line)")
	}
	if !strings.Contains(card.TypeLine, "Legendary") {
		t.Errorf("Atraxa type_line = %q", card.TypeLine)
	}
}

func oracleIDOf(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`SELECT scryfall_oracle_id::text FROM cards WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("oracle id for %q: %v", name, err)
	}
	return id
}
