package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/passwordhash"
	"manafold-backend/internal/sessioncookie"
)

// sessionTTL is how long a session row and its cookie live (ACCT-013).
const sessionTTL = 30 * 24 * time.Hour

// minPasswordLen is the registration floor (ACCT-010).
const minPasswordLen = 10

// dummyHash is verified against the supplied password when login finds no user,
// so an unknown email and a wrong password take the same time and the response
// does not become an account-existence oracle (ACCT-011). Computed once at
// startup; MustHash panics rather than leave this empty if hashing ever fails.
var dummyHash = passwordhash.MustHash("account-access timing equalizer")

type authCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionState struct {
	Authenticated bool   `json:"authenticated"`
	Email         string `json:"email,omitempty"`
}

// normalizeEmail trims surrounding whitespace and lowercases, so addresses
// differing only in case or padding resolve to one account (ACCT-019).
func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// clientIP is the rate-limit key. Each reverse proxy in front of the API appends
// the address it received the connection from to X-Forwarded-For, so the real
// client sits trustedProxyCount hops from the right of the chain; entries
// further left are client-supplied and forgeable. With fewer entries than that
// (or no header) the connection's remote address is used. trustedProxyCount
// MUST match the deployed stack's true hop count (see
// docs/intent/account-access/account-access-design.md § Rate limiting) or the
// ACCT-017 guard is only best-effort.
func clientIP(r *http.Request, trustedProxyCount int) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if idx := len(parts) - 1 - trustedProxyCount; idx >= 0 {
			if ip := strings.TrimSpace(parts[idx]); ip != "" {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// rateLimitByIP guards login and register: an empty bucket for the caller's IP
// returns 429 before any database work (ACCT-017). A nil limiter (unit tests
// that do not exercise limiting) is a pass-through.
func (a *API) rateLimitByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.LoginLimiter != nil && !a.LoginLimiter.Allow(clientIP(r, a.TrustedProxyCount)) {
			writeError(w, http.StatusTooManyRequests, "too many attempts; try again shortly")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// issueSession creates a 30-day session row for userID and sets the cookie.
func (a *API) issueSession(w http.ResponseWriter, r *http.Request, userID pgtype.UUID) error {
	expiresAt := time.Now().Add(sessionTTL)
	session, err := a.Queries.CreateSession(r.Context(), db.CreateSessionParams{
		UserID:    userID,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return err
	}
	sessioncookie.Set(w, session.ID, session.ExpiresAt.Time)
	return nil
}

// resolveSession returns the user ID named by a live session cookie, or
// ok=false when the cookie is absent, malformed, or names an expired/unknown
// session. GetSession's SQL filters expires_at > now().
func (a *API) resolveSession(r *http.Request) (pgtype.UUID, bool) {
	sessionID, ok := sessioncookie.FromRequest(r)
	if !ok {
		return pgtype.UUID{}, false
	}
	session, err := a.Queries.GetSession(r.Context(), sessionID)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return session.UserID, true
}

// @spec ACCT-010, ACCT-019
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var body authCredentials
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := normalizeEmail(body.Email)
	addr, err := mail.ParseAddress(email)
	if err != nil || email == "" || addr.Address != email {
		writeError(w, http.StatusUnprocessableEntity, "a valid email is required")
		return
	}
	if len(body.Password) < minPasswordLen {
		writeError(w, http.StatusUnprocessableEntity, "password must be at least 10 characters")
		return
	}

	if _, err := a.Queries.GetUserByEmail(r.Context(), email); err == nil {
		writeError(w, http.StatusConflict, "an account with that email already exists")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check email")
		return
	}

	hash, err := passwordhash.Hash(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user, err := a.Queries.CreateUser(r.Context(), db.CreateUserParams{
		Email:        email,
		PasswordHash: pgtype.Text{String: hash, Valid: true},
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Lost a race with a concurrent registration of the same email.
			writeError(w, http.StatusConflict, "an account with that email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	if err := a.issueSession(w, r, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusOK, sessionState{Authenticated: true, Email: user.Email})
}

// @spec ACCT-011, ACCT-019
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body authCredentials
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := normalizeEmail(body.Email)

	user, err := a.Queries.GetUserByEmail(r.Context(), email)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !user.PasswordHash.Valid) {
		// No such account, or an account with no password (a future OAuth-only
		// user). Run an equivalent verify so timing does not distinguish this
		// from a wrong password, then fail generically.
		_, _ = passwordhash.Verify(dummyHash, body.Password)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up account")
		return
	}

	ok, verifyErr := passwordhash.Verify(user.PasswordHash.String, body.Password)
	if verifyErr != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := a.issueSession(w, r, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start session")
		return
	}
	writeJSON(w, http.StatusOK, sessionState{Authenticated: true, Email: user.Email})
}

// @spec ACCT-012, ACCT-013
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if sessionID, ok := sessioncookie.FromRequest(r); ok {
		// Best-effort: a cookie naming no live session still clears cleanly.
		_ = a.Queries.DeleteSession(r.Context(), sessionID)
	}
	sessioncookie.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

// @spec ACCT-014
func (a *API) session(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.resolveSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, sessionState{Authenticated: false})
		return
	}
	user, err := a.Queries.GetUserByID(r.Context(), userID)
	if err != nil {
		// The session row outlived its user (deleted account): report logged out.
		writeJSON(w, http.StatusOK, sessionState{Authenticated: false})
		return
	}
	writeJSON(w, http.StatusOK, sessionState{Authenticated: true, Email: user.Email})
}

// @spec ACCT-021, DECK-041
func (a *API) claimDrafts(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.resolveSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		AnonToken string `json:"anon_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	token := strings.TrimSpace(body.AnonToken)
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]int64{"claimed": 0})
		return
	}

	claimed, err := a.Queries.ClaimAnonDecks(r.Context(), db.ClaimAnonDecksParams{
		UserID:    userID,
		AnonToken: pgtype.Text{String: token, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim drafts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"claimed": claimed})
}
