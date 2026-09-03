// Package server wires up the HTTP router and its routes.
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/ai"
	"manafold-backend/internal/api"
	db "manafold-backend/internal/db/generated"
	mw "manafold-backend/internal/middleware"
	"manafold-backend/internal/ratelimit"
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

	// TrustedProxyCount is the number of reverse proxies in front of the API
	// that append to X-Forwarded-For; the per-IP auth rate limiter reads the
	// client address that many hops from the right (see internal/config).
	TrustedProxyCount int
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

	// Per-IP token bucket for the auth endpoints: a burst of 10, then ~1 per
	// 6s (ACCT-017).
	h := &api.API{
		Pool:              d.Pool,
		Queries:           d.Queries,
		AI:                d.AI,
		LoginLimiter:      ratelimit.New(10, 6*time.Second),
		TrustedProxyCount: d.TrustedProxyCount,
	}

	// Unauthenticated routes: the public deck view and the /api/auth/* flow.
	r.Group(func(r chi.Router) {
		h.RegisterPublicRoutes(r)
		h.RegisterAuthRoutes(r)
	})

	// Protected /api group.
	r.Route("/api", func(r chi.Router) {
		if d.DevAuth {
			r.Use(mw.DevAuth(d.Queries))
		} else {
			r.Use(mw.AnonOrSession(d.Queries))
		}
		// @spec PLATFORM-005 — per-domain registration helpers, not a flat list.
		h.RegisterCardRoutes(r)
		h.RegisterDeckRoutes(r)
		h.RegisterImportRoutes(r)
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
