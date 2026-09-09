// Package cardsync ingests Scryfall's bulk-data exports into Manafold's own
// Postgres. cmd/cardsync calls Run; tests call it directly with local fixture
// files. Scryfall's card endpoints are never touched here beyond the two large
// bulk GETs and the manifest (CARD-008).
//
// A run downloads each export to a temporary file and closes the HTTP
// connection before it does any database write (CARD-013): the download must
// not be gated on Postgres write latency, or the connection is held open for
// the whole ingest and torn down mid-stream. The non-body phase is bounded by a
// response-header timeout and the body download by an idle deadline; neither is
// a whole-request cap over the ingest (CARD-015). The spooled export is
// then read back from disk, gzip-inflated, and decoded object by object
// (CARD-012). Oracle Cards upserts one row at a time; Default Cards is
// bulk-upserted with COPY into a session TEMP table plus one set-based merge
// (CARD-014).
//
// @spec CARD-001, CARD-002, CARD-005, CARD-006, CARD-007, CARD-012, CARD-013, CARD-014, CARD-015
package cardsync

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

const (
	defaultBaseURL = "https://api.scryfall.com"
	userAgent      = "Manafold/1.0 (github.com/Sushi-Senpai/Manafold)"
	acceptHeader   = "application/json;q=0.9,*/*;q=0.8"
)

// retryBackoff is the wait after an HTTP 429 before the single retry (CARD-006).
// It is a var so tests can shorten it.
var retryBackoff = 30 * time.Second

// downloadIdleTimeout bounds how long a bulk download may deliver no bytes
// before the run fails (CARD-015). It is deliberately not a whole-request
// timeout: that would span the ingest. A var so tests can shorten it.
var downloadIdleTimeout = 2 * time.Minute

// responseHeaderTimeout bounds the non-body phase of every bulk HTTP call — the
// manifest fetch and the wait for response headers on each bulk GET — so a
// connection that completes TLS but never sends a response status line fails
// instead of hanging a context.Background() run forever (CARD-015). It does not
// cover body reads; downloadToTemp's idle deadline does that. A var so tests can
// shorten it.
var responseHeaderTimeout = 2 * time.Minute

// tempFileDir is the directory downloadToTemp writes its spool files to; ""
// means the OS default (os.TempDir). A var so tests can point it at a scratch
// directory and assert the spool file is cleaned up (CARD-013).
var tempFileDir = ""

// Options configures a run. With OracleCardsPath / DefaultCardsPath set the run
// reads those local files — plain, uninflated JSONL or a JSON array — instead of
// downloading; otherwise it fetches the Scryfall bulk manifest, spools each
// gzip export to a temp file, and ingests it from disk.
type Options struct {
	BaseURL          string
	HTTPClient       *http.Client
	OracleCardsPath  string
	DefaultCardsPath string
}

// Result reports what a run did.
type Result struct {
	OracleUpserted int
	PrintsUpserted int
	PrintsSkipped  int
}

// Run ingests Oracle Cards into cards and Default Cards into card_prints,
// recording a sync_runs row per bulk type. A printing whose oracle_id has no
// cards row is skipped and counted, not treated as an error (CARD-005). Any
// other failure marks the sync_runs row failed with the error text and returns
// the error (CARD-007).
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (Result, error) {
	q := db.New(pool)
	var res Result

	client := opts.HTTPClient
	if client == nil {
		// No whole-request timeout: it would span the multi-minute ingest and
		// abort a healthy run. The non-body phase is bounded by a
		// response-header timeout; the body download by downloadToTemp's idle
		// deadline; neither is a whole-request cap over the ingest (CARD-015).
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		client = &http.Client{Transport: transport}
	}
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	f := &fetcher{client: client}

	var oracleUpdatedAt, defaultUpdatedAt time.Time
	var oracleRC, defaultRC io.ReadCloser

	if opts.OracleCardsPath != "" {
		file, err := os.Open(opts.OracleCardsPath)
		if err != nil {
			return res, err
		}
		oracleRC = file
	}
	if opts.DefaultCardsPath != "" {
		file, err := os.Open(opts.DefaultCardsPath)
		if err != nil {
			return res, err
		}
		defaultRC = file
	}

	if oracleRC == nil || defaultRC == nil {
		man, err := f.manifest(ctx, baseURL)
		if err != nil {
			return res, err
		}
		if oracleRC == nil {
			uri, updated, err := man.find("oracle_cards")
			if err != nil {
				return res, err
			}
			oracleUpdatedAt = updated
			path, err := f.downloadToTemp(ctx, uri)
			if err != nil {
				return res, err
			}
			defer os.Remove(path)
			rc, err := openBulkFile(path)
			if err != nil {
				return res, err
			}
			oracleRC = rc
		}
		if defaultRC == nil {
			uri, updated, err := man.find("default_cards")
			if err != nil {
				return res, err
			}
			defaultUpdatedAt = updated
			path, err := f.downloadToTemp(ctx, uri)
			if err != nil {
				return res, err
			}
			defer os.Remove(path)
			rc, err := openBulkFile(path)
			if err != nil {
				return res, err
			}
			defaultRC = rc
		}
	}
	defer oracleRC.Close()
	defer defaultRC.Close()

	oracleCount, err := ingestOracle(ctx, q, oracleRC, oracleUpdatedAt)
	if err != nil {
		return res, err
	}
	res.OracleUpserted = oracleCount

	prints, skipped, err := ingestPrints(ctx, pool, q, defaultRC, defaultUpdatedAt)
	if err != nil {
		return res, err
	}
	res.PrintsUpserted = prints
	res.PrintsSkipped = skipped

	return res, nil
}

func ingestOracle(ctx context.Context, q *db.Queries, r io.Reader, updatedAt time.Time) (int, error) {
	run, err := q.CreateSyncRun(ctx, db.CreateSyncRunParams{
		BulkType:          "oracle_cards",
		ScryfallUpdatedAt: tstz(updatedAt),
	})
	if err != nil {
		return 0, err
	}

	var count int32
	streamErr := streamJSONObjects(r, func(raw json.RawMessage) error {
		var o scryfallObject
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		if o.OracleID == "" {
			return nil
		}
		params, perr := o.toUpsertCardParams()
		if perr != nil {
			return perr
		}
		if _, err := q.UpsertCard(ctx, params); err != nil {
			return err
		}
		count++
		return nil
	})
	if streamErr != nil {
		_ = q.FailSyncRun(ctx, db.FailSyncRunParams{
			ID: run.ID, Error: text(streamErr.Error()), RowsUpserted: count,
		})
		return 0, fmt.Errorf("oracle_cards ingest: %w", streamErr)
	}
	if err := q.FinishSyncRun(ctx, db.FinishSyncRunParams{ID: run.ID, RowsUpserted: count}); err != nil {
		return 0, err
	}
	return int(count), nil
}

// ingestPrints ingests the Default Cards export into card_prints, recording a
// sync_runs row (CARD-001, CARD-007). It bulk-upserts rather than issuing a
// round trip per printing (CARD-014); a printing whose oracle_id has no cards
// row — or no parseable oracle_id at all — is skipped and counted, not treated
// as an error (CARD-005).
//
// @spec CARD-001, CARD-005, CARD-007, CARD-014
func ingestPrints(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, r io.Reader, updatedAt time.Time) (upserted, skipped int, err error) {
	run, err := q.CreateSyncRun(ctx, db.CreateSyncRunParams{
		BulkType:          "default_cards",
		ScryfallUpdatedAt: tstz(updatedAt),
	})
	if err != nil {
		return 0, 0, err
	}

	upserted, skipped, ingestErr := copyUpsertPrints(ctx, pool, r)
	if ingestErr != nil {
		_ = q.FailSyncRun(ctx, db.FailSyncRunParams{
			ID: run.ID, Error: text(ingestErr.Error()), RowsUpserted: int32(upserted),
		})
		return 0, 0, fmt.Errorf("default_cards ingest: %w", ingestErr)
	}
	if err := q.FinishSyncRun(ctx, db.FinishSyncRunParams{ID: run.ID, RowsUpserted: int32(upserted)}); err != nil {
		return 0, 0, err
	}
	return upserted, skipped, nil
}

// printImportDDL creates the session-scoped staging table copyUpsertPrints
// streams every decoded printing into. TEMP + ON COMMIT DROP means it is
// private to this transaction's connection, so concurrent syncs never collide,
// and it is gone the moment the merge commits.
const printImportDDL = `CREATE TEMP TABLE card_prints_import (
	scryfall_id      uuid NOT NULL,
	oracle_id        uuid NOT NULL,
	set_code         text NOT NULL,
	set_name         text NOT NULL,
	collector_number text NOT NULL,
	rarity           text NOT NULL,
	released_at      date,
	finishes         text[] NOT NULL,
	image_uris       jsonb,
	prices           jsonb,
	is_promo         boolean NOT NULL,
	is_reprint       boolean NOT NULL,
	is_digital       boolean NOT NULL
) ON COMMIT DROP`

// printImportColumns is the COPY column order; it must match printCopyRow.
var printImportColumns = []string{
	"scryfall_id", "oracle_id", "set_code", "set_name", "collector_number",
	"rarity", "released_at", "finishes", "image_uris", "prices",
	"is_promo", "is_reprint", "is_digital",
}

// printMergeSQL folds the staging table into card_prints in one statement. The
// JOIN on cards resolves card_id and drops orphan printings (CARD-005); the
// column list and ON CONFLICT set mirror the per-row UpsertCardPrint query so
// the resulting rows are identical. DISTINCT ON tolerates a duplicated
// scryfall_id within one export instead of failing the merge.
const printMergeSQL = `
INSERT INTO card_prints (
	scryfall_id, card_id, set_code, set_name, collector_number, rarity,
	released_at, finishes, image_uris, prices, is_promo, is_reprint, is_digital
)
SELECT DISTINCT ON (i.scryfall_id)
	i.scryfall_id, c.id, i.set_code, i.set_name, i.collector_number, i.rarity,
	i.released_at, i.finishes, i.image_uris, i.prices, i.is_promo, i.is_reprint, i.is_digital
FROM card_prints_import i
JOIN cards c ON c.scryfall_oracle_id = i.oracle_id
ORDER BY i.scryfall_id
ON CONFLICT (scryfall_id) DO UPDATE SET
	card_id          = EXCLUDED.card_id,
	set_code         = EXCLUDED.set_code,
	set_name         = EXCLUDED.set_name,
	collector_number = EXCLUDED.collector_number,
	rarity           = EXCLUDED.rarity,
	released_at      = EXCLUDED.released_at,
	finishes         = EXCLUDED.finishes,
	image_uris       = EXCLUDED.image_uris,
	prices           = EXCLUDED.prices,
	is_promo         = EXCLUDED.is_promo,
	is_reprint       = EXCLUDED.is_reprint,
	is_digital       = EXCLUDED.is_digital`

// copyUpsertPrints streams every printing from r into a TEMP staging table with
// COPY, then merges the batch into card_prints with one set-based statement
// (CARD-014). It returns the number of rows upserted (inserted or updated) and
// the number skipped: printings with an unparseable oracle_id (dropped before
// staging) plus staged printings whose oracle_id matched no cards row
// (CARD-005).
func copyUpsertPrints(ctx context.Context, pool *pgxpool.Pool, r io.Reader) (upserted, skipped int, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, printImportDDL); err != nil {
		return 0, 0, fmt.Errorf("create staging table: %w", err)
	}

	stream := newJSONObjectStream(r)
	var malformed int // printings with no usable oracle_id: skipped before staging
	src := pgx.CopyFromFunc(func() ([]any, error) {
		for {
			raw, nerr := stream.next()
			if errors.Is(nerr, io.EOF) {
				return nil, nil
			}
			if nerr != nil {
				return nil, nerr
			}
			var o scryfallObject
			if uerr := json.Unmarshal(raw, &o); uerr != nil {
				return nil, uerr
			}
			oracleUUID, ok := parseUUID(o.OracleID)
			if !ok {
				malformed++
				continue
			}
			return o.printCopyRow(oracleUUID), nil
		}
	})

	staged, err := tx.CopyFrom(ctx, pgx.Identifier{"card_prints_import"}, printImportColumns, src)
	if err != nil {
		return 0, 0, fmt.Errorf("stage printings: %w", err)
	}

	tag, err := tx.Exec(ctx, printMergeSQL)
	if err != nil {
		return 0, 0, fmt.Errorf("merge printings: %w", err)
	}
	upserted = int(tag.RowsAffected())

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}

	// Every staged row that did not merge is an orphan printing (its oracle_id
	// is not in cards); add the rows dropped before staging. Scryfall's exports
	// carry no duplicate printing ids, so staged - upserted is the orphan count.
	skipped = malformed + int(staged) - upserted
	return upserted, skipped, nil
}

// jsonObjectStream decodes Scryfall card objects one at a time, so a
// multi-hundred-MB export never lands in memory whole. It accepts both shapes
// the project ingests: the bulk exports are newline-delimited JSON (a
// json.Decoder consumes consecutive values across the separating newlines
// natively, so no per-line length limit applies), while the dev / CI seed file
// (backend/seed/cards.json) is a single JSON array of card objects, the shape
// Scryfall's card APIs return. The first non-whitespace byte picks the path: a
// '[' means unwrap the array and stream its elements; anything else is decoded
// as consecutive top-level values (CARD-012, CARD-040).
type jsonObjectStream struct {
	dec     *json.Decoder
	array   bool
	started bool
	done    bool
}

func newJSONObjectStream(r io.Reader) *jsonObjectStream {
	br := bufio.NewReader(r)
	array := false
	if prefix, _ := br.Peek(512); len(prefix) > 0 {
		for _, b := range prefix {
			if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
				continue
			}
			array = b == '['
			break
		}
	}
	return &jsonObjectStream{dec: json.NewDecoder(br), array: array}
}

// next returns the next raw object, or io.EOF when the stream is exhausted.
func (s *jsonObjectStream) next() (json.RawMessage, error) {
	if s.done {
		return nil, io.EOF
	}
	if s.array && !s.started {
		s.started = true
		if _, err := s.dec.Token(); err != nil { // consume the opening '['
			s.done = true
			return nil, err
		}
	}
	if s.array && !s.dec.More() {
		s.done = true
		return nil, io.EOF
	}
	var raw json.RawMessage
	if err := s.dec.Decode(&raw); err != nil {
		s.done = true
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	return raw, nil
}

// streamJSONObjects invokes fn for every object newJSONObjectStream yields.
func streamJSONObjects(r io.Reader, fn func(json.RawMessage) error) error {
	s := newJSONObjectStream(r)
	for {
		raw, err := s.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(raw); err != nil {
			return err
		}
	}
}

// ---- HTTP ----------------------------------------------------------------

type fetcher struct {
	client *http.Client
}

type bulkManifest struct {
	Data []struct {
		Type string `json:"type"`
		// JSONLDownloadURI points at a gzip-compressed newline-delimited-JSON
		// export on data.scryfall.io. Scryfall retired the array-form
		// download_uri; jsonl_download_uri is the current field (CARD-001).
		JSONLDownloadURI string    `json:"jsonl_download_uri"`
		UpdatedAt        time.Time `json:"updated_at"`
	} `json:"data"`
}

func (m bulkManifest) find(bulkType string) (uri string, updatedAt time.Time, err error) {
	for _, d := range m.Data {
		if d.Type == bulkType {
			if d.JSONLDownloadURI == "" {
				return "", time.Time{}, fmt.Errorf("bulk manifest %q entry has no jsonl_download_uri", bulkType)
			}
			return d.JSONLDownloadURI, d.UpdatedAt, nil
		}
	}
	return "", time.Time{}, fmt.Errorf("bulk manifest has no %q entry", bulkType)
}

func (f *fetcher) manifest(ctx context.Context, baseURL string) (bulkManifest, error) {
	body, err := f.get(ctx, baseURL+"/bulk-data")
	if err != nil {
		return bulkManifest{}, err
	}
	defer body.Close()
	var m bulkManifest
	if err := json.NewDecoder(body).Decode(&m); err != nil {
		return bulkManifest{}, fmt.Errorf("decode bulk manifest: %w", err)
	}
	return m, nil
}

// get issues a GET carrying the required Scryfall etiquette headers, retrying
// once after retryBackoff on an HTTP 429 (CARD-006).
func (f *fetcher) get(ctx context.Context, url string) (io.ReadCloser, error) {
	resp, err := f.do(ctx, url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		select {
		case <-time.After(retryBackoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp, err = f.do(ctx, url)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("scryfall GET %s: unexpected status %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}

// downloadToTemp streams a bulk export to a temporary file and returns its
// path, closing the HTTP connection before the caller performs any database
// write (CARD-013). Scryfall serves the export as application/gzip; the temp
// file holds it still compressed. The download is bounded by an idle deadline,
// not a whole-request timeout spanning the ingest (CARD-015). On any failure
// the partial temp file is removed; on success the caller owns the file and
// must remove it.
//
// @spec CARD-001, CARD-013, CARD-015
func (f *fetcher) downloadToTemp(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	body, err := f.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer body.Close()

	tmp, err := os.CreateTemp(tempFileDir, "cardsync-*.jsonl.gz")
	if err != nil {
		return "", err
	}

	idle := newIdleReader(body, downloadIdleTimeout, cancel)
	defer idle.stop()

	if _, err := io.Copy(tmp, idle); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("scryfall bulk %s: download: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// idleReader invokes onIdle if the wrapped reader delivers no data within d.
// It backs downloadToTemp's idle deadline (CARD-015): each read that returns
// bytes rearms the timer, so a stalled connection trips it while a slow-but-
// alive one does not.
type idleReader struct {
	r     io.Reader
	d     time.Duration
	timer *time.Timer
}

func newIdleReader(r io.Reader, d time.Duration, onIdle func()) *idleReader {
	return &idleReader{r: r, d: d, timer: time.AfterFunc(d, onIdle)}
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(ir.d)
	}
	return n, err
}

func (ir *idleReader) stop() { ir.timer.Stop() }

// openBulkFile opens a temp file written by downloadToTemp and returns a reader
// over its gunzipped contents (the export is gzip on the wire and on disk;
// CARD-012). A file that is not valid gzip is an error, not garbage read.
// Closing the returned ReadCloser closes both the gzip reader and the file.
func openBulkFile(path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("cardsync bulk file %s: gunzip: %w", path, err)
	}
	return gzipBody{Reader: zr, body: file}, nil
}

// gzipBody couples a gzip.Reader to the HTTP body it inflates so a single Close
// tears down both.
type gzipBody struct {
	*gzip.Reader
	body io.Closer
}

func (g gzipBody) Close() error {
	zerr := g.Reader.Close()
	berr := g.body.Close()
	if zerr != nil {
		return zerr
	}
	return berr
}

func (f *fetcher) do(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", acceptHeader)
	return f.client.Do(req)
}

// ---- pgtype helpers ----------------------------------------------------

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, u.Valid
}

func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func textPtr(p *string) pgtype.Text {
	if p == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *p, Valid: true}
}

func tstz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func dateFrom(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func int4Ptr(p *int32) pgtype.Int4 {
	if p == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *p, Valid: true}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
