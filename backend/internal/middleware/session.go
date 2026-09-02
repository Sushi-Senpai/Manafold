package middleware

import (
	"net/http"

	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/sessioncookie"
)

// SessionAuth reads the session cookie, validates it against GetSession (whose
// SQL filters expires_at > now(), so "no rows" covers missing and expired in
// one check), and attaches the user ID via authctx. It fails closed: any
// failure responds 401 and returns without calling next, because every handler
// downstream trusts that authctx.UserID was set and never checks the ok-bool.
//
// M1 runs on DevAuth; SessionAuth is exercised by tests (an unauthenticated
// request must 401) and is the default path from M3.
//
// @spec ACCT-003
func SessionAuth(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionID, ok := sessioncookie.FromRequest(r)
			if !ok {
				http.Error(w, "not authenticated", http.StatusUnauthorized)
				return
			}
			session, err := queries.GetSession(r.Context(), sessionID)
			if err != nil {
				http.Error(w, "not authenticated", http.StatusUnauthorized)
				return
			}
			ctx := authctx.WithUserID(r.Context(), session.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
