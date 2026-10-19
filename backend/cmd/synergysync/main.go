// Command synergysync is the Archidekt synergy batch job (docs/intent/synergy/).
// It is built into the same image as cmd/api and run by hand or as a scheduled
// job with no HTTP surface. It samples recent public Commander decks from
// Archidekt for the configured commanders and their colour-identity baselines,
// at no more than one request per 6 s, and stores per-commander synergy
// aggregates (SYN-001..032). Each run is bounded by SYNERGY_MAX_REQUESTS and
// resumes where the last one stopped.
//
// Environment (all optional except DATABASE_URL):
//
//	SYNERGY_COMMANDERS_FILE               commander list, one name per line (default: embedded list)
//	SYNERGY_DECKS_PER_COMMANDER           default 60
//	SYNERGY_SEARCH_PAGES                  default 3
//	SYNERGY_BASELINE_COMMANDERS           default 5
//	SYNERGY_DECKS_PER_BASELINE_COMMANDER  default 12
//	SYNERGY_MAX_REQUESTS                  default 600
//	SYNERGY_REFRESH_DAYS                  default 7
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/config"
	appdb "manafold-backend/internal/db"
	"manafold-backend/internal/synergy"
)

// envPosInt reads a strictly positive integer, falling back to 0 (the
// synergy.Options default) when unset or invalid.
func envPosInt(key string) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func main() {
	cfg, err := config.LoadCardsync()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	commanders, err := synergy.LoadCommanders(os.Getenv("SYNERGY_COMMANDERS_FILE"))
	if err != nil {
		log.Fatalf("commander list: %v", err)
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

	opts := synergy.Options{
		Commanders:                commanders,
		DecksPerCommander:         envPosInt("SYNERGY_DECKS_PER_COMMANDER"),
		SearchPages:               envPosInt("SYNERGY_SEARCH_PAGES"),
		BaselineCommanders:        envPosInt("SYNERGY_BASELINE_COMMANDERS"),
		DecksPerBaselineCommander: envPosInt("SYNERGY_DECKS_PER_BASELINE_COMMANDER"),
		MaxRequests:               envPosInt("SYNERGY_MAX_REQUESTS"),
		RefreshAfter:              time.Duration(envPosInt("SYNERGY_REFRESH_DAYS")) * 24 * time.Hour,
	}

	res, err := synergy.Run(ctx, pool, opts)
	if err != nil {
		log.Fatalf("synergy sync: %v (%d requests, %d targets refreshed, %d pending)",
			err, res.RequestsMade, res.TargetsRefreshed, res.TargetsPending)
	}
	log.Printf("synergy sync complete: %d requests, %d targets refreshed, %d pending, %d unresolved commanders",
		res.RequestsMade, res.TargetsRefreshed, res.TargetsPending, len(res.Unresolved))
}
