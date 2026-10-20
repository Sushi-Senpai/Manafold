package synergy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock replaces the pacing clock and sleeper so tests observe the waits
// without spending them: sleeping advances the clock.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func useFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	fc := &fakeClock{t: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	oldNow, oldSleep := now, sleepCtx
	now = func() time.Time { fc.mu.Lock(); defer fc.mu.Unlock(); return fc.t }
	sleepCtx = func(ctx context.Context, d time.Duration) error {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		fc.sleeps = append(fc.sleeps, d)
		fc.t = fc.t.Add(d)
		return ctx.Err()
	}
	t.Cleanup(func() { now, sleepCtx = oldNow, oldSleep })
	return fc
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// @spec SYN-002, SYN-003
func TestClient_SendsIdentifyingHeadersAndPublicSearchQuery(t *testing.T) {
	useFakeClock(t)
	var gotUA, gotAccept, gotAuth, gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotAccept, gotAuth, gotURI = r.Header.Get("User-Agent"), r.Header.Get("Accept"), r.Header.Get("Authorization"), r.URL.RequestURI()
		_, _ = w.Write(fixture(t, "search_page1.json"))
	}))
	defer srv.Close()

	c := newArchidektClient(srv.URL, nil, 10)
	if _, err := c.search(context.Background(), "Krenko, Mob Boss", 1); err != nil {
		t.Fatal(err)
	}
	if gotUA != "Manafold-synergy/1.0 (+https://github.com/Sushi-Senpai/Manafold)" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if strings.Contains(strings.ToLower(gotUA), "@") {
		t.Errorf("User-Agent carries what looks like personal data: %q", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotAuth != "" {
		t.Errorf("request carried Authorization %q; reads must be unauthenticated", gotAuth)
	}
	for _, want := range []string{"/api/decks/v3/?", "deckFormat=3", "commanderName=Krenko%2C+Mob+Boss", "orderBy=-createdAt"} {
		if !strings.Contains(gotURI, want) {
			t.Errorf("search URI %q lacks %q", gotURI, want)
		}
	}
}

// @spec SYN-003
func TestClient_StartsRequestsAtLeastSixSecondsApart(t *testing.T) {
	fc := useFakeClock(t)
	var starts []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, now())
		_, _ = w.Write(fixture(t, "search_page2.json"))
	}))
	defer srv.Close()

	c := newArchidektClient(srv.URL, nil, 10)
	for i := 0; i < 3; i++ {
		if _, err := c.search(context.Background(), "Krenko, Mob Boss", 2); err != nil {
			t.Fatal(err)
		}
	}
	if len(starts) != 3 {
		t.Fatalf("server saw %d requests", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < 6*time.Second {
			t.Errorf("request %d started %v after the previous, want >= 6s", i, gap)
		}
	}
	if len(fc.sleeps) != 2 {
		t.Errorf("slept %d times, want 2 (none before the first request)", len(fc.sleeps))
	}
}

// @spec SYN-004
func TestClient_429BacksOffEscalatingThenRetriesTheSameRequest(t *testing.T) {
	fc := useFakeClock(t)
	var calls int
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		uris = append(uris, r.URL.RequestURI())
		if calls <= 3 { // three 429s, no Retry-After
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(fixture(t, "search_page2.json"))
	}))
	defer srv.Close()

	c := newArchidektClient(srv.URL, nil, 10)
	if _, err := c.search(context.Background(), "Krenko, Mob Boss", 2); err != nil {
		t.Fatalf("search after three 429s: %v", err)
	}
	if calls != 4 || c.requests != 4 {
		t.Errorf("calls=%d requests counted=%d, want 4 and 4 (retries count against the budget)", calls, c.requests)
	}
	for _, u := range uris {
		if u != uris[0] {
			t.Errorf("retry changed the request: %q vs %q", u, uris[0])
		}
	}
	var backoffs []time.Duration
	for _, d := range fc.sleeps {
		if d >= time.Minute {
			backoffs = append(backoffs, d)
		}
	}
	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute}
	if len(backoffs) != 3 || backoffs[0] != want[0] || backoffs[1] != want[1] || backoffs[2] != want[2] {
		t.Errorf("back-offs = %v, want %v", backoffs, want)
	}
}

// @spec SYN-004
func TestClient_FourthConsecutive429FailsTheRequest(t *testing.T) {
	useFakeClock(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newArchidektClient(srv.URL, nil, 10)
	_, err := c.search(context.Background(), "Krenko, Mob Boss", 1)
	if err == nil || errors.Is(err, errBudgetExhausted) {
		t.Fatalf("err = %v, want a persistent-429 failure", err)
	}
	if calls != 4 {
		t.Errorf("server saw %d requests, want 4 (first + three retries)", calls)
	}
}

// @spec SYN-005
func TestClient_DeckGoneIsSkippedOtherErrorsFail(t *testing.T) {
	useFakeClock(t)
	for _, status := range []int{403, 404, 410} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		c := newArchidektClient(srv.URL, nil, 10)
		if _, found, err := c.deck(context.Background(), 1); err != nil || found {
			t.Errorf("status %d: found=%v err=%v, want skipped", status, found, err)
		}
		srv.Close()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, _, err := newArchidektClient(srv.URL, nil, 10).deck(context.Background(), 1); err == nil {
		t.Error("HTTP 500 must fail, not be skipped")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer bad.Close()
	if _, _, err := newArchidektClient(bad.URL, nil, 10).deck(context.Background(), 1); err == nil {
		t.Error("an undecodable body must fail")
	}
}

// @spec SYN-034
func TestClient_StopsAtTheJobRequestCap(t *testing.T) {
	useFakeClock(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t, "search_page2.json"))
	}))
	defer srv.Close()

	c := newArchidektClient(srv.URL, nil, 0)
	if _, err := c.search(context.Background(), "x", 1); !errors.Is(err, errBudgetExhausted) {
		t.Errorf("before any job: err = %v, want errBudgetExhausted", err)
	}
	c.beginJob(2)
	for i := 0; i < 2; i++ {
		if _, err := c.search(context.Background(), "x", 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.search(context.Background(), "x", 1); !errors.Is(err, errBudgetExhausted) {
		t.Errorf("err = %v, want errBudgetExhausted", err)
	}

	// A new job gets a fresh cap, and the 6 s gap still holds across the
	// boundary because the pacing clock carries over.
	c.beginJob(1)
	before := c.lastStart
	if _, err := c.search(context.Background(), "x", 1); err != nil {
		t.Fatal(err)
	}
	if gap := c.lastStart.Sub(before); gap < 6*time.Second {
		t.Errorf("first request of the next job started %v after the last, want >= 6s", gap)
	}
}
