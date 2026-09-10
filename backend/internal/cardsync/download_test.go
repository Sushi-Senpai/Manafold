package cardsync

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestBulkDownload_SpoolDecouplesIngestFromTheConnection reproduces the reported
// failure — the Default Cards pass dying partway with a mid-stream stream error
// because every HTTP body read waited on a per-row Postgres write — and proves
// the temp-file spool fixes it. No Postgres required: the slow DB writer is
// modelled as a paced reader, and the CDN "resetting a slow long-lived stream"
// as a server that abandons a response it cannot deliver in time.
//
// @spec CARD-013, CARD-015
func TestBulkDownload_SpoolDecouplesIngestFromTheConnection(t *testing.T) {
	gz, lines := syntheticGzipNDJSON(t, 16<<20)

	// The server abandons the connection if the whole body is not delivered
	// within this window — long enough for an unpaced drain, far too short for
	// one paced at ingest speed.
	srv := newAbandoningServer(t, gz, 400*time.Millisecond)
	f := &fetcher{client: srv.Client()}

	// 1. The pre-fix pattern: read the live HTTP body at DB-write pace. The
	//    server gives up mid-body and the read fails — the reported symptom.
	body, err := f.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	buf := make([]byte, 512<<10)
	_, err = io.CopyBuffer(io.Discard, &pacedReader{r: body, delay: 15 * time.Millisecond}, buf)
	body.Close()
	if err == nil {
		t.Fatal("reading the live body at ingest pace completed cleanly; the " +
			"slow-consumer connection reset is not being reproduced (socket " +
			"buffer larger than the payload?)")
	}
	t.Logf("pre-fix pattern failed as expected: %v", err)

	// 2. The fix: spool to a temp file first (fast, unpaced), then ingest from
	//    disk at any pace. The download completes and every object is readable.
	path, err := f.downloadToTemp(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("downloadToTemp: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("spool file missing after a successful download: %v", err)
	}

	rc, err := openBulkFile(path)
	if err != nil {
		t.Fatalf("openBulkFile: %v", err)
	}
	defer rc.Close()

	var got int
	err = streamJSONObjects(rc, func(json.RawMessage) error {
		got++
		return nil
	})
	if err != nil {
		t.Fatalf("streamJSONObjects from the spool: %v", err)
	}
	if got != lines {
		t.Fatalf("decoded %d objects from the spool, want all %d", got, lines)
	}
}

// TestDownloadToTemp_IdleConnectionFailsAndCleansUp covers the replacement for
// the removed 15-minute whole-request timeout: a connection that stalls after
// the first bytes trips the idle deadline, the run fails, and the partial spool
// file is removed.
//
// @spec CARD-013, CARD-015
func TestDownloadToTemp_IdleConnectionFailsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	restoreDir, restoreTimeout := tempFileDir, downloadIdleTimeout
	tempFileDir, downloadIdleTimeout = dir, 150*time.Millisecond
	t.Cleanup(func() { tempFileDir, downloadIdleTimeout = restoreDir, restoreTimeout })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "\x1f\x8b\x08") // a few bytes, then nothing
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	f := &fetcher{client: srv.Client()}
	if _, err := f.downloadToTemp(context.Background(), srv.URL); err == nil {
		t.Fatal("downloadToTemp returned nil error for a stalled connection")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("spool dir still holds %d file(s) after a failed download; want it cleaned", len(entries))
	}
}

// TestDownloadToTemp_TotalDeadlineFailsSlowTrickleAndCleansUp covers the
// backstop to the idle deadline: a server that dribbles a byte often enough to
// keep the idle timer alive would otherwise hang a context.Background() run
// forever. The generous total deadline trips instead, the run fails, and the
// partial spool file is removed.
//
// @spec CARD-013, CARD-015
func TestDownloadToTemp_TotalDeadlineFailsSlowTrickleAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	restoreDir, restoreIdle, restoreTotal := tempFileDir, downloadIdleTimeout, downloadTotalTimeout
	tempFileDir = dir
	downloadIdleTimeout = time.Second
	downloadTotalTimeout = 250 * time.Millisecond
	t.Cleanup(func() {
		tempFileDir, downloadIdleTimeout, downloadTotalTimeout = restoreDir, restoreIdle, restoreTotal
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(40 * time.Millisecond):
			}
			if _, err := w.Write([]byte{0}); err != nil {
				return
			}
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	f := &fetcher{client: srv.Client()}
	if _, err := f.downloadToTemp(context.Background(), srv.URL); err == nil {
		t.Fatal("downloadToTemp returned nil error for a slow-trickle connection")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("spool dir still holds %d file(s) after a failed download; want it cleaned", len(entries))
	}
}

// TestDownloadToTemp_RoundTripsGzipBody is the success path with no Postgres: a
// gzip-served body is spooled, read back through openBulkFile, and every object
// decodes; the caller's os.Remove then clears the spool file.
//
// @spec CARD-012, CARD-013
func TestDownloadToTemp_RoundTripsGzipBody(t *testing.T) {
	dir := t.TempDir()
	restore := tempFileDir
	tempFileDir = dir
	t.Cleanup(func() { tempFileDir = restore })

	lines := []string{
		`{"id":"c0000000-0000-0000-0000-0000000000a1","oracle_id":"11111111-1111-1111-1111-111111111111","name":"Alpha"}`,
		`{"id":"c0000000-0000-0000-0000-0000000000a2","oracle_id":"22222222-2222-2222-2222-222222222222","name":"Beta"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		zw := gzip.NewWriter(w)
		for _, ln := range lines {
			_, _ = io.WriteString(zw, ln+"\n")
		}
		_ = zw.Close()
	}))
	t.Cleanup(srv.Close)

	f := &fetcher{client: srv.Client()}
	path, err := f.downloadToTemp(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("downloadToTemp: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("spool file missing: %v", err)
	}

	rc, err := openBulkFile(path)
	if err != nil {
		t.Fatalf("openBulkFile: %v", err)
	}
	var got int
	if err := streamJSONObjects(rc, func(json.RawMessage) error { got++; return nil }); err != nil {
		t.Fatalf("streamJSONObjects: %v", err)
	}
	rc.Close()
	if got != len(lines) {
		t.Fatalf("decoded %d objects, want %d", got, len(lines))
	}

	os.Remove(path)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("spool dir not empty after cleanup: %d file(s)", len(entries))
	}
}

// pacedReader models a slow downstream consumer: it sleeps before every Read,
// so draining a live HTTP body through it takes as long as the writes it stands
// in for.
type pacedReader struct {
	r     io.Reader
	delay time.Duration
}

func (p *pacedReader) Read(b []byte) (int, error) {
	time.Sleep(p.delay)
	return p.r.Read(b)
}

// newAbandoningServer serves body with a real Content-Length but drops the
// connection mid-response if the client has not drained it within budget —
// what Go surfaces to the caller as an unexpected-EOF / stream error, the same
// class as the reported PROTOCOL_ERROR.
func newAbandoningServer(t *testing.T, body []byte, budget time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()

		hdr := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/gzip\r\n"+
			"Content-Length: %d\r\nConnection: close\r\n\r\n", len(body))
		if _, err := conn.Write([]byte(hdr)); err != nil {
			return
		}

		deadline := time.Now().Add(budget)
		const chunk = 32 << 10
		for off := 0; off < len(body); off += chunk {
			if time.Now().After(deadline) {
				return // client too slow: abandon the response mid-body
			}
			end := min(off+chunk, len(body))
			_ = conn.SetWriteDeadline(deadline)
			if _, err := conn.Write(body[off:end]); err != nil {
				return
			}
		}
		_ = conn.SetWriteDeadline(time.Time{})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// syntheticGzipNDJSON builds at least minRaw bytes of high-entropy
// newline-delimited card-printing JSON and returns it gzip-compressed (the
// wire/disk form) plus the object count. The payload is deliberately
// incompressible so it stays larger than any socket buffer once gzipped.
func syntheticGzipNDJSON(t *testing.T, minRaw int) (gz []byte, lines int) {
	t.Helper()
	rng := mrand.New(mrand.NewSource(1))
	fill := make([]byte, 96)
	id := make([]byte, 16)
	oid := make([]byte, 16)

	var raw bytes.Buffer
	raw.Grow(minRaw + 4096)
	for raw.Len() < minRaw {
		_, _ = rng.Read(fill)
		_, _ = rng.Read(id)
		_, _ = rng.Read(oid)
		fmt.Fprintf(&raw,
			`{"id":%q,"oracle_id":%q,"name":"Synthetic","set":"tst","_fill":%q}`+"\n",
			uuidish(id), uuidish(oid), base64.StdEncoding.EncodeToString(fill))
		lines++
	}

	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return out.Bytes(), lines
}

func uuidish(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
