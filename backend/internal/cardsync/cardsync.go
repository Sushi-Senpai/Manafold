// Package cardsync ingests Scryfall's bulk-data exports into Manafold's own
// Postgres. cmd/cardsync calls Run; tests call it directly with local fixture
// files. Scryfall's card endpoints are never touched here beyond the two large
// streamed bulk GETs and the manifest (CARD-008).
//
// @spec CARD-001, CARD-002, CARD-005, CARD-006, CARD-007
package cardsync

import (
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
// reads those local files instead of downloading; otherwise it fetches the
// Scryfall bulk manifest and streams the exports.
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
			rc, err := f.get(ctx, uri)
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
			rc, err := f.get(ctx, uri)
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
	streamErr := streamObjects(r, func(raw json.RawMessage) error {
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
	streamErr := streamObjects(r, func(raw json.RawMessage) error {
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

// streamObjects decodes a Scryfall bulk file — a single JSON array of card
// objects — one element at a time, so a multi-hundred-MB export never lands in
// memory whole.
func streamObjects(r io.Reader, fn func(json.RawMessage) error) error {
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return fmt.Errorf("expected a JSON array, got %v", tok)
	}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if err := fn(raw); err != nil {
			return err
		}
	}
	_, err = dec.Token() // closing ]
	return err
}

// ---- HTTP ----------------------------------------------------------------

type fetcher struct {
	client *http.Client
}

type bulkManifest struct {
	Data []struct {
		Type        string    `json:"type"`
		DownloadURI string    `json:"download_uri"`
		UpdatedAt   time.Time `json:"updated_at"`
	} `json:"data"`
}

func (m bulkManifest) find(bulkType string) (uri string, updatedAt time.Time, err error) {
	for _, d := range m.Data {
		if d.Type == bulkType {
			return d.DownloadURI, d.UpdatedAt, nil
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
