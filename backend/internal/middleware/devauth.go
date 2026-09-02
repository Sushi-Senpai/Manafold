// Package middleware holds the auth middleware. M1 ships DevAuth only; the
// session-backed SessionAuth is wired in for M3 (see
// docs/intent/account-access/).
package middleware

import (
	"net/http"

	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
)

// devUserEmail is the fixed identity DevAuth signs every request in as.
const (
	devUserEmail = "dev@manafold.local"
	devUserName  = "Dev User"
)

// DevAuth stands in for real authentication: it upserts one fixed user and
// attaches its ID to every request via authctx. No login flow, no cookie, no
// token. It runs only when DEV_AUTH=true (off by default — see
// internal/config); the real email + password login (M3) replaces it with
// SessionAuth.
//
// @spec ACCT-001
func DevAuth(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := queries.UpsertDevUser(r.Context(), db.UpsertDevUserParams{
				Email: devUserEmail,
				Name:  devUserName,
			})
			if err != nil {
				http.Error(w, "failed to resolve dev user", http.StatusInternalServerError)
				return
			}
			ctx := authctx.WithUserID(r.Context(), user.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
