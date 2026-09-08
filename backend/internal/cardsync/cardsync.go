// Package cardsync ingests Scryfall's bulk-data exports into Manafold's own
// Postgres. cmd/cardsync calls Run; tests call it directly with local fixture
// files. Scryfall's card endpoints are never touched here beyond the two large
// streamed bulk GETs and the manifest (CARD-008). Each downloaded export is a
// gzip-compressed newline-delimited-JSON stream that Run inflates and decodes
// object by object (CARD-012).
//
// @spec CARD-001, CARD-002, CARD-005, CARD-006, CARD-007, CARD-012
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

// Options configures a run. With OracleCardsPath / DefaultCardsPath set the run
// reads those local files — plain, uninflated JSONL — instead of downloading;
// otherwise it fetches the Scryfall bulk manifest and streams the gzip-inflated
// exports.
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
		client = &http.Client{Timeout: 15 * time.Minute}
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
			rc, err := f.getBulk(ctx, uri)
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
			rc, err := f.getBulk(ctx, uri)
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

	prints, skipped, err := ingestPrints(ctx, q, defaultRC, defaultUpdatedAt)
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

func ingestPrints(ctx context.Context, q *db.Queries, r io.Reader, updatedAt time.Time) (upserted, skipped int, err error) {
	run, err := q.CreateSyncRun(ctx, db.CreateSyncRunParams{
		BulkType:          "default_cards",
		ScryfallUpdatedAt: tstz(updatedAt),
	})
	if err != nil {
		return 0, 0, err
	}

	var count int32
	var skips int
	streamErr := streamJSONObjects(r, func(raw json.RawMessage) error {
		var o scryfallObject
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		oracleUUID, ok := parseUUID(o.OracleID)
		if !ok {
			skips++
			return nil
		}
		card, gerr := q.GetCardByScryfallOracleID(ctx, oracleUUID)
		if gerr != nil {
			if errors.Is(gerr, pgx.ErrNoRows) {
				skips++
				return nil
			}
			return gerr
		}
		if _, err := q.UpsertCardPrint(ctx, o.toUpsertCardPrintParams(card.ID)); err != nil {
			return err
		}
		count++
		return nil
	})
	if streamErr != nil {
		_ = q.FailSyncRun(ctx, db.FailSyncRunParams{
			ID: run.ID, Error: text(streamErr.Error()), RowsUpserted: count,
		})
		return 0, 0, fmt.Errorf("default_cards ingest: %w", streamErr)
	}
	if err := q.FinishSyncRun(ctx, db.FinishSyncRunParams{ID: run.ID, RowsUpserted: count}); err != nil {
		return 0, 0, err
	}
	return int(count), skips, nil
}

// streamJSONObjects decodes a stream of Scryfall card objects one at a time, so
// a multi-hundred-MB export never lands in memory whole. It accepts both shapes
// the project ingests: the bulk exports are newline-delimited JSON (a
// json.Decoder consumes consecutive values across the separating newlines
// natively, so no per-line length limit applies), while the dev / CI seed file
// (backend/seed/cards.json) is a single JSON array of card objects, the shape
// Scryfall's card APIs return. The first non-whitespace byte picks the path: a
// '[' means unwrap the array and stream its elements; anything else is decoded
// as consecutive top-level values (CARD-012, CARD-040).
func streamJSONObjects(r io.Reader, fn func(json.RawMessage) error) error {
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

	dec := json.NewDecoder(br)
	if array {
		if _, err := dec.Token(); err != nil { // consume the opening '['
			return err
		}
	}
	for {
		if array && !dec.More() {
			return nil
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
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

// getBulk downloads a bulk export and returns a reader over its inflated
// contents. Scryfall serves the export as application/gzip with no
// Content-Encoding, so net/http never inflates it and the job must (CARD-012).
// Closing the returned ReadCloser closes both the gzip reader and the body.
func (f *fetcher) getBulk(ctx context.Context, url string) (io.ReadCloser, error) {
	body, err := f.get(ctx, url)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(body)
	if err != nil {
		body.Close()
		return nil, fmt.Errorf("scryfall bulk %s: gunzip: %w", url, err)
	}
	return gzipBody{Reader: zr, body: body}, nil
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
