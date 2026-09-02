// Package server wires up the HTTP router and its routes.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/ai"
	"manafold-backend/internal/api"
	db "manafold-backend/internal/db/generated"
	mw "manafold-backend/internal/middleware"
)

// Deps bundles everything the router needs.
type Deps struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
	AI      *ai.Client

	// DevAuth selects internal/middleware.DevAuth (fixed local-dev user)
	// instead of session-based auth for every protected /api route — off by
	// default (see internal/config). M1 runs with DevAuth true.
	DevAuth bool
}

// New builds the chi router for the API.
func New(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	// No CORS middleware: the browser only ever reaches this backend through
	// the frontend's same-origin /api/* rewrite (@spec PLATFORM-004,
	// ACCT-016), and the session cookie is scoped to that frontend origin.

	// @spec PLATFORM-002
	r.Get("/health", healthHandler(d.Pool))

	h := &api.API{Pool: d.Pool, Queries: d.Queries, AI: d.AI}

	// Unauthenticated read-only routes (public deck view).
	r.Group(func(r chi.Router) {
		h.RegisterPublicRoutes(r)
	})

	// Protected /api group.
	r.Route("/api", func(r chi.Router) {
		if d.DevAuth {
			r.Use(mw.DevAuth(d.Queries))
		} else {
			r.Use(mw.SessionAuth(d.Queries))
		}
		// @spec PLATFORM-005 — per-domain registration helpers, not a flat list.
		h.RegisterCardRoutes(r)
		h.RegisterDeckRoutes(r)
	})

	return r
}

// healthHandler pings Postgres on every call, so /health reports both "server
// up" and "database reachable".
func healthHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		code := http.StatusOK
		if pool == nil {
			status, code = "database unreachable", http.StatusServiceUnavailable
		} else if err := pool.Ping(r.Context()); err != nil {
			status, code = "database unreachable", http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
	}
}
