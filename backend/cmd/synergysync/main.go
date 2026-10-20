// Command synergysync gathers synergy data for one commander by hand
// (docs/intent/synergy/). Synergy is normally gathered on demand by the
// background worker inside cmd/api; this runs the same worker code for the
// named commander in the foreground, at the same one-request-per-6-s pace, for
// development or to gather a commander ahead of time (SYN-037).
//
//	go run ./cmd/synergysync "Krenko, Mob Boss"
//
// It needs only DATABASE_URL, applies migrations first like cmd/cardsync, and
// exits non-zero if the name does not resolve to a commander, another worker
// holds the lock, or the job fails. SYNERGY_* tuning variables are read as in
// the API; SYNERGY_ENABLED is not needed.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/config"
	appdb "manafold-backend/internal/db"
	"manafold-backend/internal/synergy"
)

func main() {
	name := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if name == "" {
		log.Fatalf("usage: synergysync \"<commander name>\"")
	}

	cfg, err := config.LoadCardsync()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := appdb.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	opts := synergy.OptionsFromEnv()
	opts.Enabled = true
	svc := synergy.NewService(pool, opts)

	oracle, resolved, found, err := svc.ResolveCommander(ctx, name)
	if err != nil {
		log.Fatalf("resolve %q: %v", name, err)
	}
	if !found {
		log.Fatalf("%q is not a commander in the card mirror (run cmd/cardsync first?)", name)
	}
	queued, err := svc.Enqueue(ctx, oracle)
	if err != nil {
		log.Fatalf("queue %q: %v", resolved, err)
	}
	if !queued {
		log.Printf("%s already has a queued or running job; draining the queue", resolved)
	}

	res, err := svc.Drain(ctx)
	switch {
	case errors.Is(err, synergy.ErrWorkerBusy):
		log.Fatalf("another synergy worker holds the lock (is the API running with SYNERGY_ENABLED=true?); the job stays queued for it")
	case err != nil:
		log.Fatalf("synergy gather: %v (%d jobs done, %d requests)", err, res.Jobs, res.Requests)
	}
	log.Printf("synergy gather complete: %d job(s), %d requests", res.Jobs, res.Requests)
}
