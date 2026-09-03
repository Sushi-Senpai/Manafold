// Package middleware holds the auth middleware: AnonOrSession (the default —
// session cookie, then anonymous-draft token, else 401) and DevAuth (a fixed
// local-dev / CI user, active only when DEV_AUTH=true). See
// docs/intent/account-access/.
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
// token. It runs only when DEV_AUTH=true (off by default, and never in a
// deployed environment — see internal/config); AnonOrSession is the real
// default.
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
