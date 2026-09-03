package middleware

import (
	"net/http"

	"manafold-backend/internal/authctx"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/sessioncookie"
)

// AnonTokenHeader carries an anonymous-draft token for a caller with no session
// (ACCT-020).
const AnonTokenHeader = "X-Anon-Token"

// AnonOrSession is the default protected-group middleware. It resolves the
// caller in priority order:
//
//  1. a valid, unexpired session cookie -> authctx.WithUserID (GetSession's SQL
//     filters expires_at > now(), so "no rows" covers missing and expired);
//  2. otherwise a non-empty X-Anon-Token header -> authctx.WithAnonToken;
//  3. otherwise 401, without calling next.
//
// A valid session always wins over a supplied token. The middleware fails
// closed: every handler downstream trusts that exactly one identity is set.
//
// @spec ACCT-003, ACCT-020
func AnonOrSession(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sessionID, ok := sessioncookie.FromRequest(r); ok {
				if session, err := queries.GetSession(r.Context(), sessionID); err == nil {
					ctx := authctx.WithUserID(r.Context(), session.UserID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			if token := r.Header.Get(AnonTokenHeader); token != "" {
				ctx := authctx.WithAnonToken(r.Context(), token)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			http.Error(w, "not authenticated", http.StatusUnauthorized)
		})
	}
}
