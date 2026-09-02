// Package sessioncookie owns the session cookie's name and flags so the code
// that sets it on login (internal/api, from M3) and the code that reads it on
// every protected request (internal/middleware) never drift apart.
package sessioncookie

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Name is the session cookie's name.
const Name = "manafold_session"

// Set writes the session cookie. SameSite=Lax is sufficient because the
// frontend's same-origin proxy (ACCT-016) makes this a first-party cookie of
// the frontend origin; Secure stays unconditional since browsers treat
// localhost as a secure context.
func Set(w http.ResponseWriter, sessionID pgtype.UUID, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     Name,
		Value:    uuidString(sessionID),
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Clear expires the session cookie immediately.
func Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     Name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// FromRequest reads and parses the session cookie's value as a UUID. ok is
// false if the cookie is absent or malformed — callers treat both as "no
// session".
func FromRequest(r *http.Request) (id pgtype.UUID, ok bool) {
	cookie, err := r.Cookie(Name)
	if err != nil {
		return pgtype.UUID{}, false
	}
	if err := id.Scan(cookie.Value); err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}

func uuidString(id pgtype.UUID) string {
	s, _ := id.Value()
	str, _ := s.(string)
	return str
}
