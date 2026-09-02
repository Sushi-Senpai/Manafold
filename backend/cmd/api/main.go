// Command api is the Manafold HTTP server entry point. It follows a fixed
// startup order — load config, apply every embedded migration, open the pool,
// then serve — so the binary that expects a schema is the one that applied it
// (PLATFORM-001, PLATFORM-003).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/ai"
	"manafold-backend/internal/config"
	appdb "manafold-backend/internal/db"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// @spec PLATFORM-001 — migrate before the request-serving pool is opened.
	if err := appdb.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	handler := server.New(server.Deps{
		Pool:    pool,
		Queries: db.New(pool),
		AI:      ai.NewClient(),
		DevAuth: cfg.DevAuth,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("manafold-api listening on :%s (dev_auth=%v)", cfg.Port, cfg.DevAuth)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}
