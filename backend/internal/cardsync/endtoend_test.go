package cardsync_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"manafold-backend/internal/cardsync"
	db "manafold-backend/internal/db/generated"
)

// TestRun_EndToEnd_ScryfallShapedGzipManifest drives a full sync the way a live
// run works: fetch the /bulk-data manifest, follow each entry's
// jsonl_download_uri, download a body served as application/gzip with NO
// Content-Encoding (so net/http never inflates it), gunzip it, decode the
// newline-delimited JSON, ingest, and — the user-visible payoff — confirm the
// mirror is populated and card autocomplete now returns names. This is the
// regression the merged app hit: the old download_uri + JSON-array reader left
// find() returning "" and the mirror empty, so search returned nothing.
//
// @spec CARD-001, CARD-012, CARD-020
func TestRun_EndToEnd_ScryfallShapedGzipManifest(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	oracleJSONL := mustReadFile(t, "testdata/oracle_cards.jsonl")
	defaultJSONL := mustReadFile(t, "testdata/default_cards.jsonl")

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/bulk-data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"type":"oracle_cards","updated_at":"2026-09-06T09:01:54.221+00:00",
			 "jsonl_download_uri":"` + base + `/oracle-cards.jsonl.gz"},
			{"type":"default_cards","updated_at":"2026-09-06T09:05:27.982+00:00",
			 "jsonl_download_uri":"` + base + `/default-cards.jsonl.gz"}
		]}`))
	})
	serveGz := func(payload []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// application/gzip, and deliberately NO Content-Encoding header,
			// exactly as data.scryfall.io serves the exports.
			w.Header().Set("Content-Type", "application/gzip")
			zw := gzip.NewWriter(w)
			_, _ = zw.Write(payload)
			_ = zw.Close()
		}
	}
	mux.HandleFunc("/oracle-cards.jsonl.gz", serveGz(oracleJSONL))
	mux.HandleFunc("/default-cards.jsonl.gz", serveGz(defaultJSONL))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL

	res, err := cardsync.Run(ctx, pool, cardsync.Options{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("cardsync.Run against Scryfall-shaped gzip manifest: %v", err)
	}
	if res.OracleUpserted != 4 || res.PrintsUpserted != 2 || res.PrintsSkipped != 1 {
		t.Fatalf("Run result = %+v, want {OracleUpserted:4 PrintsUpserted:2 PrintsSkipped:1}", res)
	}

	q := db.New(pool)

	// The user-visible symptom of the bug was empty search. Autocomplete must
	// now resolve the freshly-ingested names.
	names, err := q.AutocompleteCardNames(ctx, "Manafold Test")
	if err != nil {
		t.Fatalf("autocomplete: %v", err)
	}
	if len(names) < 4 {
		t.Fatalf("autocomplete %q returned %v, want the 4 ingested Manafold Test cards", "Manafold Test", names)
	}
	t.Logf("mirror populated: OracleUpserted=%d PrintsUpserted=%d PrintsSkipped=%d; autocomplete(%q) -> %v",
		res.OracleUpserted, res.PrintsUpserted, res.PrintsSkipped, "Manafold Test", names)

	oracleRun, err := q.LatestSyncRun(ctx, "oracle_cards")
	if err != nil {
		t.Fatalf("latest oracle_cards sync run: %v", err)
	}
	if oracleRun.Status != "succeeded" {
		t.Fatalf("oracle_cards sync run status = %q, want succeeded", oracleRun.Status)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return bytes.TrimSpace(b)
}
