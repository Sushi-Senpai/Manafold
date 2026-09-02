// Command cardsync is the standalone Scryfall bulk-data ingestion binary. It is
// built into the same image as cmd/api and run as a scheduled job with no HTTP
// surface (PLATFORM-006). It downloads the Oracle Cards and Default Cards bulk
// exports, upserts them into cards / card_prints, and records a sync_runs row
// (CARD-001, CARD-007).
package main

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/cardsync"
	"manafold-backend/internal/config"
	appdb "manafold-backend/internal/db"
)

func main() {
	cfg, err := config.Load()
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

	res, err := cardsync.Run(ctx, pool, cardsync.Options{})
	if err != nil {
		log.Fatalf("card sync: %v", err)
	}
	log.Printf("card sync complete: %d cards, %d prints upserted, %d prints skipped",
		res.OracleUpserted, res.PrintsUpserted, res.PrintsSkipped)
}
