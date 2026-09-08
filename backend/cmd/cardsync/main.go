// Command cardsync is the standalone Scryfall bulk-data ingestion binary. It is
// built into the same image as cmd/api and run as a scheduled job with no HTTP
// surface (PLATFORM-006). It downloads the Oracle Cards and Default Cards bulk
// exports, upserts them into cards / card_prints, and records a sync_runs row
// (CARD-001, CARD-007). For a dev / CI seed it instead reads a local card
// fixture named by CARDSYNC_SEED_PATH (or the per-pass CARDSYNC_ORACLE_PATH /
// CARDSYNC_DEFAULT_PATH) — see seedOptions and backend/seed/cards.json
// (CARD-040).
package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/cardsync"
	"manafold-backend/internal/config"
	appdb "manafold-backend/internal/db"
)

// seedOptions reads the local-file overrides for a dev / CI seed run. With
// CARDSYNC_SEED_PATH set, the one file (a JSON array of Scryfall card objects —
// see backend/seed/cards.json) feeds both the oracle and printing passes;
// CARDSYNC_ORACLE_PATH / CARDSYNC_DEFAULT_PATH set them individually. With none
// set the run downloads the real Scryfall bulk exports (CARD-001).
func seedOptions() cardsync.Options {
	oracle := os.Getenv("CARDSYNC_ORACLE_PATH")
	dflt := os.Getenv("CARDSYNC_DEFAULT_PATH")
	if seed := os.Getenv("CARDSYNC_SEED_PATH"); seed != "" {
		if oracle == "" {
			oracle = seed
		}
		if dflt == "" {
			dflt = seed
		}
	}
	return cardsync.Options{OracleCardsPath: oracle, DefaultCardsPath: dflt}
}

func main() {
	cfg, err := config.LoadCardsync()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := appdb.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	res, err := cardsync.Run(ctx, pool, seedOptions())
	if err != nil {
		log.Fatalf("card sync: %v", err)
	}
	log.Printf("card sync complete: %d cards, %d prints upserted, %d prints skipped",
		res.OracleUpserted, res.PrintsUpserted, res.PrintsSkipped)
}
