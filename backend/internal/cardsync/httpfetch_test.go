package cardsync

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// TestManifest_ResolvesJSONLDownloadURI covers the manifest decode + lookup the
// sync job does before every bulk download: it must read the current
// jsonl_download_uri field (Scryfall retired the array-form download_uri) and
// must fail loudly, not return an empty URL, when an entry lacks it (CARD-001).
//
// @spec CARD-001
func TestManifest_ResolvesJSONLDownloadURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bulk-data" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[
			{"type":"oracle_cards","updated_at":"2026-09-06T09:01:54.221+00:00",
			 "jsonl_download_uri":"https://data.scryfall.io/oracle-cards/oracle-cards-20260906.jsonl.gz"},
			{"type":"default_cards","updated_at":"2026-09-06T09:05:27.982+00:00"}
		]}`)
	}))
	t.Cleanup(srv.Close)

	f := &fetcher{client: srv.Client()}
	man, err := f.manifest(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}

	uri, updated, err := man.find("oracle_cards")
	if err != nil {
		t.Fatalf("find oracle_cards: %v", err)
	}
	if uri != "https://data.scryfall.io/oracle-cards/oracle-cards-20260906.jsonl.gz" {
		t.Errorf("oracle_cards uri = %q, want the jsonl_download_uri value", uri)
	}
	if updated.IsZero() {
		t.Error("oracle_cards updated_at parsed as zero, want the manifest timestamp")
	}

	if _, _, err := man.find("default_cards"); err == nil {
		t.Error("find on an entry with no jsonl_download_uri returned nil error, want a failure")
	}
	if _, _, err := man.find("rulings"); err == nil {
		t.Error("find on an absent bulk type returned nil error, want a failure")
	}
}

// TestOpenBulkFile_InflatesGzippedJSONL exercises the ingest-side read: a bulk
// export is spooled to disk still gzip-compressed (Scryfall serves it as
// application/gzip with no Content-Encoding), so openBulkFile must inflate the
// file itself, and the inflated stream is newline-delimited JSON — one object
// per line — that streamJSONObjects walks without buffering the whole file
// (CARD-001, CARD-012).
//
// @spec CARD-001, CARD-012
func TestOpenBulkFile_InflatesGzippedJSONL(t *testing.T) {
	lines := []string{
		`{"oracle_id":"11111111-1111-1111-1111-111111111111","name":"Alpha"}`,
		`{"oracle_id":"22222222-2222-2222-2222-222222222222","name":"Beta"}`,
		`{"oracle_id":"33333333-3333-3333-3333-333333333333","name":"Gamma"}`,
	}

	path := filepath.Join(t.TempDir(), "bulk.jsonl.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp bulk file: %v", err)
	}
	zw := gzip.NewWriter(file)
	for _, ln := range lines {
		_, _ = io.WriteString(zw, ln+"\n")
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	body, err := openBulkFile(path)
	if err != nil {
		t.Fatalf("openBulkFile: %v", err)
	}
	t.Cleanup(func() { _ = body.Close() })

	var names []string
	err = streamJSONObjects(body, func(raw json.RawMessage) error {
		var o struct {
			OracleID string `json:"oracle_id"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		names = append(names, o.Name)
		return nil
	})
	if err != nil {
		t.Fatalf("streamJSONObjects over inflated file: %v", err)
	}
	if strings.Join(names, ",") != "Alpha,Beta,Gamma" {
		t.Errorf("decoded names = %v, want [Alpha Beta Gamma]", names)
	}
}

// TestOpenBulkFile_NonGzipIsAnError confirms a spooled file that is not valid
// gzip is surfaced as an error (so CARD-007 fails the run) rather than read as
// garbage.
//
// @spec CARD-007, CARD-012
func TestOpenBulkFile_NonGzipIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-gzip.jsonl.gz")
	if err := os.WriteFile(path, []byte(`{"not":"gzip"}`), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := openBulkFile(path); err == nil {
		t.Fatal("openBulkFile on a non-gzip file returned nil error, want a gunzip failure")
	}
}

// TestStreamJSONObjects_DecodesNewlineDelimited pins the JSONL decode contract
// directly: consecutive objects separated by newlines are each delivered once,
// trailing whitespace is tolerated, and an empty stream is not an error
// (CARD-012).
//
// @spec CARD-012
func TestStreamJSONObjects_DecodesNewlineDelimited(t *testing.T) {
	in := "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n\n"
	var got []int
	err := streamJSONObjects(strings.NewReader(in), func(raw json.RawMessage) error {
		var o struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		got = append(got, o.N)
		return nil
	})
	if err != nil {
		t.Fatalf("streamJSONObjects: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("decoded = %v, want [1 2 3]", got)
	}

	if err := streamJSONObjects(strings.NewReader(""), func(json.RawMessage) error {
		t.Fatal("callback ran on an empty stream")
		return nil
	}); err != nil {
		t.Errorf("empty stream returned %v, want nil", err)
	}
}

// TestStreamJSONObjects_DecodesTopLevelArray pins the other shape the ingest
// accepts: backend/seed/cards.json is a single JSON array of card objects (the
// shape Scryfall's card APIs return, and what the dev / CI seed run feeds
// through both the oracle and printing passes). Each element must be delivered
// once, with leading whitespace tolerated and the enclosing brackets stripped
// (CARD-012, CARD-040).
func TestStreamJSONObjects_DecodesTopLevelArray(t *testing.T) {
	in := "\n  [\n  {\"n\":1},\n  {\"n\":2},\n  {\"n\":3}\n]\n"
	var got []int
	err := streamJSONObjects(strings.NewReader(in), func(raw json.RawMessage) error {
		var o struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		got = append(got, o.N)
		return nil
	})
	if err != nil {
		t.Fatalf("streamJSONObjects over top-level array: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("decoded = %v, want [1 2 3]", got)
	}

	if err := streamJSONObjects(strings.NewReader("[]"), func(json.RawMessage) error {
		t.Fatal("callback ran on an empty array")
		return nil
	}); err != nil {
		t.Errorf("empty array returned %v, want nil", err)
	}
}
