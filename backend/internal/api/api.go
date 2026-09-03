// Package api holds the HTTP handlers for the card and deck domains and the
// unauthenticated public deck view. Each domain registers its own routes
// through a helper (RegisterCardRoutes, RegisterDeckRoutes) so the router in
// internal/server stays a wiring file, not a flat route list (PLATFORM-005).
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"manafold-backend/internal/ai"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/ratelimit"
)

// API carries the dependencies every handler needs. server.New builds it once
// and calls the Register* helpers.
type API struct {
	Pool         *pgxpool.Pool
	Queries      *db.Queries
	AI           *ai.Client
	LoginLimiter *ratelimit.Limiter
}

// RegisterCardRoutes mounts the card-data read endpoints. It is called inside
// the authenticated /api group.
//
// @spec CARD-021, CARD-020, PLATFORM-005
func (a *API) RegisterCardRoutes(r chi.Router) {
	r.Get("/cards/search", a.searchCards)
	r.Get("/cards/autocomplete", a.autocompleteCards)
}

// RegisterDeckRoutes mounts the deck CRUD and validation endpoints. It is
// called inside the authenticated /api group.
//
// @spec DECK-001, DECK-009, DECK-051, PLATFORM-005
func (a *API) RegisterDeckRoutes(r chi.Router) {
	r.Get("/decks", a.listDecks)
	r.Post("/decks", a.createDeck)
	r.Get("/decks/{id}", a.getDeck)
	r.Patch("/decks/{id}", a.updateDeck)
	r.Put("/decks/{id}/commander", a.setCommander)
	r.Post("/decks/{id}/cards", a.addCard)
	r.Delete("/decks/{id}/cards/{cardId}", a.removeCard)
	r.Get("/decks/{id}/validation", a.getValidation)
	r.Get("/decks/{id}/stats", a.getDeckStats)
}

// RegisterPublicRoutes mounts the read-only public deck view at the router
// root, deliberately outside the authenticated /api group so an unauthenticated
// client can read a deck its owner marked public.
//
// @spec DECK-030, DECK-031
func (a *API) RegisterPublicRoutes(r chi.Router) {
	r.Get("/public/decks/{id}", a.getPublicDeck)
}

// RegisterAuthRoutes mounts the email + password endpoints as flat leaf routes
// under /api/auth, deliberately outside the authenticated /api group: register
// and login establish a session, session and logout must work without one, and
// claim-drafts resolves the session from the cookie itself. login and register
// carry the per-IP rate limiter. The routes are registered as absolute paths
// (not a nested Route) so they sit beside — not under — the protected /api
// mount.
//
// @spec ACCT-010, ACCT-011, ACCT-012, ACCT-014, ACCT-017, ACCT-021, PLATFORM-005
func (a *API) RegisterAuthRoutes(r chi.Router) {
	r.With(a.rateLimitByIP).Post("/api/auth/register", a.register)
	r.With(a.rateLimitByIP).Post("/api/auth/login", a.login)
	r.Post("/api/auth/logout", a.logout)
	r.Get("/api/auth/session", a.session)
	r.Post("/api/auth/claim-drafts", a.claimDrafts)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
