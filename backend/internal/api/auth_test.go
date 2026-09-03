package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	mw "manafold-backend/internal/middleware"
	"manafold-backend/internal/ratelimit"
	"manafold-backend/internal/sessioncookie"
)

// authRouter mirrors server.New's wiring for the slice these tests exercise:
// /api/auth/* unauthenticated, the deck routes behind AnonOrSession. Building it
// here (rather than importing internal/server) avoids the server -> api import
// cycle.
func authRouter(a *API) http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		a.RegisterAuthRoutes(r)
	})
	r.Route("/api", func(r chi.Router) {
		r.Use(mw.AnonOrSession(a.Queries))
		a.RegisterDeckRoutes(r)
	})
	return r
}

type reqOpt func(*http.Request)

func withCookies(cs []*http.Cookie) reqOpt {
	return func(r *http.Request) {
		for _, c := range cs {
			r.AddCookie(c)
		}
	}
}

func withHeader(k, v string) reqOpt {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

func do(t *testing.T, h http.Handler, method, path string, body any, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) []*http.Cookie {
	t.Helper()
	cs := rec.Result().Cookies()
	for _, c := range cs {
		if c.Name == sessioncookie.Name && c.Value != "" {
			return cs
		}
	}
	t.Fatalf("response set no %s cookie: %v", sessioncookie.Name, cs)
	return nil
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return "acct-" + hex.EncodeToString(randBytes(t, 8)) + "@manafold.test"
}

// @spec ACCT-010, ACCT-013
func TestAuth_RegisterCreatesAccountAndSession(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(100, time.Minute)
	h := authRouter(a)
	email := uniqueEmail(t)

	rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": email, "password": "a-strong-passphrase",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register = %d %s, want 200", rec.Code, rec.Body.String())
	}
	got := decode[sessionState](t, rec)
	if !got.Authenticated || got.Email != email {
		t.Fatalf("register body = %+v, want authenticated for %s", got, email)
	}

	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessioncookie.Name {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("register set no session cookie")
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("session cookie flags = %+v, want HttpOnly+Secure+Lax+Path=/", cookie)
	}
	// GET /api/auth/session with the cookie reports the account.
	rec = do(t, h, http.MethodGet, "/api/auth/session", nil, withCookies([]*http.Cookie{cookie}))
	if s := decode[sessionState](t, rec); !s.Authenticated || s.Email != email {
		t.Fatalf("session = %+v, want authenticated for %s", s, email)
	}
}

// @spec ACCT-010, ACCT-019
func TestAuth_RegisterRejectsDuplicateAndWeakInput(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(100, time.Minute)
	h := authRouter(a)
	email := uniqueEmail(t)

	if rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": email, "password": "a-strong-passphrase",
	}); rec.Code != http.StatusOK {
		t.Fatalf("first register = %d %s", rec.Code, rec.Body.String())
	}

	if rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": email, "password": "another-strong-one",
	}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate register = %d %s, want 409", rec.Code, rec.Body.String())
	}
	// Case/whitespace-insensitive: the same address padded and upper-cased still collides (ACCT-019).
	if rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "  " + toUpper(email) + " ", "password": "yet-another-one",
	}); rec.Code != http.StatusConflict {
		t.Fatalf("case-variant register = %d %s, want 409", rec.Code, rec.Body.String())
	}

	if rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": uniqueEmail(t), "password": "short",
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short-password register = %d, want 422", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": "not-an-email", "password": "a-strong-passphrase",
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad-email register = %d, want 422", rec.Code)
	}
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// @spec ACCT-011
func TestAuth_LoginVerifiesPasswordAndIsGenericOnFailure(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(100, time.Minute)
	h := authRouter(a)
	email := uniqueEmail(t)
	const pw = "the-correct-passphrase"

	if rec := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": email, "password": pw,
	}); rec.Code != http.StatusOK {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}

	rec := do(t, h, http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": pw})
	if rec.Code != http.StatusOK {
		t.Fatalf("login (correct) = %d %s, want 200", rec.Code, rec.Body.String())
	}
	sessionCookie(t, rec)

	wrongPw := do(t, h, http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": "wrong"})
	unknown := do(t, h, http.MethodPost, "/api/auth/login", map[string]string{"email": uniqueEmail(t), "password": pw})
	if wrongPw.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password=%d unknown-email=%d, want both 401", wrongPw.Code, unknown.Code)
	}
	if wrongPw.Body.String() != unknown.Body.String() {
		t.Fatalf("login failure bodies differ (enumeration oracle): %q vs %q", wrongPw.Body.String(), unknown.Body.String())
	}
	for _, rec := range []*httptest.ResponseRecorder{wrongPw, unknown} {
		if len(rec.Result().Cookies()) != 0 {
			t.Fatalf("failed login set a cookie: %v", rec.Result().Cookies())
		}
	}
}

// @spec ACCT-012, ACCT-014
func TestAuth_LogoutClearsSessionAndSessionEndpointNeverFails(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(100, time.Minute)
	h := authRouter(a)
	email := uniqueEmail(t)

	reg := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{"email": email, "password": "a-strong-passphrase"})
	cookies := sessionCookie(t, reg)

	rec := do(t, h, http.MethodPost, "/api/auth/logout", nil, withCookies(cookies))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", rec.Code)
	}

	// The now-deleted session reports logged out, still 200.
	rec = do(t, h, http.MethodGet, "/api/auth/session", nil, withCookies(cookies))
	if rec.Code != http.StatusOK {
		t.Fatalf("session after logout = %d, want 200", rec.Code)
	}
	if s := decode[sessionState](t, rec); s.Authenticated {
		t.Fatalf("session after logout = %+v, want not authenticated", s)
	}

	// No cookie at all: still 200, not 401.
	rec = do(t, h, http.MethodGet, "/api/auth/session", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("session with no cookie = %d, want 200", rec.Code)
	}

	// Logout with no live session still succeeds.
	rec = do(t, h, http.MethodPost, "/api/auth/logout", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout with no cookie = %d, want 204", rec.Code)
	}
}

// @spec ACCT-017
func TestAuth_LoginRateLimitedPerIP(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(3, time.Hour)
	h := authRouter(a)

	body := map[string]string{"email": uniqueEmail(t), "password": "does-not-matter"}
	codes := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		codes = append(codes, do(t, h, http.MethodPost, "/api/auth/login", body,
			withHeader("X-Forwarded-For", "203.0.113.7")).Code)
	}
	// First 3 reach the handler (401 unknown account), then the bucket is empty.
	if codes[0] != http.StatusUnauthorized || codes[2] != http.StatusUnauthorized {
		t.Fatalf("first three login codes = %v, want 401s", codes[:3])
	}
	if codes[3] != http.StatusTooManyRequests || codes[4] != http.StatusTooManyRequests {
		t.Fatalf("4th/5th login codes = %v, want 429", codes[3:])
	}
	// A different IP has its own bucket.
	if rec := do(t, h, http.MethodPost, "/api/auth/login", body, withHeader("X-Forwarded-For", "198.51.100.9")); rec.Code == http.StatusTooManyRequests {
		t.Fatal("a fresh IP was rate limited")
	}
}

// @spec ACCT-003, ACCT-020, DECK-040
func TestAuth_AnonymousDraftScopedToToken(t *testing.T) {
	a := testAPI(t)
	h := authRouter(a)
	const tokenHeader = "X-Anon-Token"
	mine := "anon-" + hex.EncodeToString(randBytes(t, 8))
	theirs := "anon-" + hex.EncodeToString(randBytes(t, 8))

	// No session and no token -> 401 (ACCT-003).
	if rec := do(t, h, http.MethodGet, "/api/decks", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-identity GET /api/decks = %d, want 401", rec.Code)
	}

	// With a token, an anonymous visitor can create and read a deck.
	rec := do(t, h, http.MethodPost, "/api/decks", map[string]string{"name": "Anon Draft"}, withHeader(tokenHeader, mine))
	if rec.Code != http.StatusCreated {
		t.Fatalf("anon create deck = %d %s", rec.Code, rec.Body.String())
	}
	deckID := decode[deckJSON](t, rec).ID

	if rec := do(t, h, http.MethodGet, "/api/decks/"+deckID, nil, withHeader(tokenHeader, mine)); rec.Code != http.StatusOK {
		t.Fatalf("owner token GET deck = %d %s, want 200", rec.Code, rec.Body.String())
	}
	// A different token must not see it (same 404 as a wrong user).
	if rec := do(t, h, http.MethodGet, "/api/decks/"+deckID, nil, withHeader(tokenHeader, theirs)); rec.Code != http.StatusNotFound {
		t.Fatalf("other token GET deck = %d, want 404", rec.Code)
	}
	// It shows up in the owning token's list, not the other's.
	if l := decode[struct {
		Decks []deckJSON `json:"decks"`
	}](t, do(t, h, http.MethodGet, "/api/decks", nil, withHeader(tokenHeader, mine))); len(l.Decks) != 1 || l.Decks[0].ID != deckID {
		t.Fatalf("owning token deck list = %+v, want exactly the anon draft", l.Decks)
	}
	if l := decode[struct {
		Decks []deckJSON `json:"decks"`
	}](t, do(t, h, http.MethodGet, "/api/decks", nil, withHeader(tokenHeader, theirs))); len(l.Decks) != 0 {
		t.Fatalf("other token deck list = %+v, want empty", l.Decks)
	}
}

// @spec ACCT-021, DECK-041
func TestAuth_ClaimDraftsReassignsAndIsIdempotent(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(100, time.Minute)
	h := authRouter(a)
	const tokenHeader = "X-Anon-Token"
	token := "anon-" + hex.EncodeToString(randBytes(t, 8))

	// Build two anonymous drafts under the token.
	var draftIDs []string
	for _, name := range []string{"Draft One", "Draft Two"} {
		rec := do(t, h, http.MethodPost, "/api/decks", map[string]string{"name": name}, withHeader(tokenHeader, token))
		if rec.Code != http.StatusCreated {
			t.Fatalf("anon create %q = %d %s", name, rec.Code, rec.Body.String())
		}
		draftIDs = append(draftIDs, decode[deckJSON](t, rec).ID)
	}

	// Register, then claim the token.
	reg := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": uniqueEmail(t), "password": "a-strong-passphrase",
	})
	cookies := sessionCookie(t, reg)

	rec := do(t, h, http.MethodPost, "/api/auth/claim-drafts", map[string]string{"anon_token": token}, withCookies(cookies))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim-drafts = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if c := decode[struct {
		Claimed int64 `json:"claimed"`
	}](t, rec); c.Claimed != 2 {
		t.Fatalf("claimed = %d, want 2", c.Claimed)
	}

	// The signed-in user now owns both, by session cookie alone.
	for _, id := range draftIDs {
		if rec := do(t, h, http.MethodGet, "/api/decks/"+id, nil, withCookies(cookies)); rec.Code != http.StatusOK {
			t.Fatalf("claimed deck %s via session = %d, want 200", id, rec.Code)
		}
	}
	// The token now owns nothing.
	if rec := do(t, h, http.MethodGet, "/api/decks/"+draftIDs[0], nil, withHeader(tokenHeader, token)); rec.Code != http.StatusNotFound {
		t.Fatalf("stale token GET claimed deck = %d, want 404", rec.Code)
	}

	// Idempotent: a second claim of the same token moves nothing.
	rec = do(t, h, http.MethodPost, "/api/auth/claim-drafts", map[string]string{"anon_token": token}, withCookies(cookies))
	if c := decode[struct {
		Claimed int64 `json:"claimed"`
	}](t, rec); rec.Code != http.StatusOK || c.Claimed != 0 {
		t.Fatalf("second claim = %d claimed=%d, want 200 / 0", rec.Code, c.Claimed)
	}

	// claim-drafts without a session is 401.
	if rec := do(t, h, http.MethodPost, "/api/auth/claim-drafts", map[string]string{"anon_token": token}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated claim-drafts = %d, want 401", rec.Code)
	}
}

// @spec ACCT-020
func TestAuth_SessionWinsOverAnonToken(t *testing.T) {
	a := testAPI(t)
	a.LoginLimiter = ratelimit.New(100, time.Minute)
	h := authRouter(a)
	const tokenHeader = "X-Anon-Token"

	// A registered user with one deck, held by session cookie.
	reg := do(t, h, http.MethodPost, "/api/auth/register", map[string]string{
		"email": uniqueEmail(t), "password": "a-strong-passphrase",
	})
	cookies := sessionCookie(t, reg)
	rec := do(t, h, http.MethodPost, "/api/decks", map[string]string{"name": "Real Deck"}, withCookies(cookies))
	deckID := decode[deckJSON](t, rec).ID

	// Same request also carrying a bogus anon token: the session still wins, so
	// the user's own deck resolves.
	rec = do(t, h, http.MethodGet, "/api/decks/"+deckID, nil, withCookies(cookies), withHeader(tokenHeader, "some-unrelated-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("session+token GET own deck = %d %s, want 200 (session wins)", rec.Code, rec.Body.String())
	}
}
