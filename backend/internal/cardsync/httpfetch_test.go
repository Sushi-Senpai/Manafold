package cardsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestFetcher_SendsEtiquetteHeadersAndRetriesOn429 exercises the real HTTP path
// the sync job uses for every outbound Scryfall request: it must carry a
// descriptive User-Agent and an explicit Accept header, and on an HTTP 429 it
// must wait before making exactly one retry (CARD-006). The backoff is shortened
// via the package var so the test does not actually sleep 30 seconds.
//
// @spec CARD-006
func TestFetcher_SendsEtiquetteHeadersAndRetriesOn429(t *testing.T) {
	orig := retryBackoff
	retryBackoff = 20 * time.Millisecond
	t.Cleanup(func() { retryBackoff = orig })

	var mu sync.Mutex
	var seen []struct {
		userAgent string
		accept    string
		at        time.Time
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, struct {
			userAgent string
			accept    string
			at        time.Time
		}{r.Header.Get("User-Agent"), r.Header.Get("Accept"), time.Now()})
		n := len(seen)
		mu.Unlock()

		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	f := &fetcher{client: srv.Client()}
	body, err := f.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("get after one 429: %v", err)
	}
	t.Cleanup(func() { _ = body.Close() })

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("body = %q, want the success payload from the retry", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("server saw %d requests, want exactly 2 (original + one retry)", len(seen))
	}
	for i, s := range seen {
		if s.userAgent == "" || s.userAgent != userAgent {
			t.Errorf("request %d User-Agent = %q, want %q", i, s.userAgent, userAgent)
		}
		if s.accept == "" || s.accept != acceptHeader {
			t.Errorf("request %d Accept = %q, want %q", i, s.accept, acceptHeader)
		}
	}
	if gap := seen[1].at.Sub(seen[0].at); gap < retryBackoff {
		t.Errorf("retry came after %s, want at least the %s backoff", gap, retryBackoff)
	}
}

// TestFetcher_NonRetryableStatusIsAnError confirms a non-200, non-429 status is
// surfaced as an error rather than a partial read, so CARD-007 can mark the run
// failed.
//
// @spec CARD-006, CARD-007
func TestFetcher_NonRetryableStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	f := &fetcher{client: srv.Client()}
	if _, err := f.get(context.Background(), srv.URL); err == nil {
		t.Fatal("get on a 500 returned nil error, want a non-nil error")
	}
}
