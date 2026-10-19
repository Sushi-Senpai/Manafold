// Command cardsync is the standalone Scryfall bulk-data ingestion binary. It is
// built into the same image as cmd/api and run as a scheduled job with no HTTP
// surface (PLATFORM-006). It downloads the Oracle Cards, Default Cards, and
// Oracle Tags bulk exports, upserts them into cards / card_prints and the
// oracle-tag tables, and records a sync_runs row per export (CARD-001,
// CARD-007, CARD-010). For a dev / CI seed it instead reads a local card
// fixture named by CARDSYNC_SEED_PATH (or the per-pass CARDSYNC_ORACLE_PATH /
// CARDSYNC_DEFAULT_PATH) and its tag fixture (CARDSYNC_TAGS_PATH, or the
// oracle_tags.json beside the seed) — see seedOptions, backend/seed/cards.json,
// and backend/seed/oracle_tags.json (CARD-040, CARD-066).
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/cardsync"
	"manafold-backend/internal/config"
	appdb "manafold-backend/internal/db"
)

// seedOptions reads the local-file overrides for a dev / CI seed run. With
// CARDSYNC_SEED_PATH set, the one file (a JSON array of Scryfall card objects —
// see backend/seed/cards.json) feeds both the oracle and printing passes;
// CARDSYNC_ORACLE_PATH / CARDSYNC_DEFAULT_PATH set them individually.
// CARDSYNC_TAGS_PATH names the oracle-tags file; without it, a seed run uses the
// oracle_tags.json beside the seed file when one exists (see
// backend/seed/oracle_tags.json), so the one seed variable still yields a fully
// offline run (CARD-066). With none set the run downloads the real Scryfall
// bulk exports (CARD-001, CARD-010).
func seedOptions() cardsync.Options {
	oracle := os.Getenv("CARDSYNC_ORACLE_PATH")
	dflt := os.Getenv("CARDSYNC_DEFAULT_PATH")
	tags := os.Getenv("CARDSYNC_TAGS_PATH")
	if seed := os.Getenv("CARDSYNC_SEED_PATH"); seed != "" {
		if oracle == "" {
			oracle = seed
		}
		if dflt == "" {
			dflt = seed
		}
		if tags == "" {
			sibling := filepath.Join(filepath.Dir(seed), "oracle_tags.json")
			if _, err := os.Stat(sibling); err == nil {
				tags = sibling
			}
		}
	}
	return cardsync.Options{OracleCardsPath: oracle, DefaultCardsPath: dflt, OracleTagsPath: tags}
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
	log.Printf("card sync complete: %d cards, %d prints upserted, %d prints skipped; %d oracle tags, %d taggings upserted, %d taggings skipped",
		res.OracleUpserted, res.PrintsUpserted, res.PrintsSkipped, res.TagsUpserted, res.TaggingsUpserted, res.TaggingsSkipped)
}
