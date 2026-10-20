package synergy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://archidekt.com"

	// userAgent identifies the project honestly and carries no personal data
	// (SYN-003).
	userAgent    = "Manafold-synergy/1.0 (+https://github.com/Sushi-Senpai/Manafold)"
	acceptHeader = "application/json"

	// commanderFormat is Archidekt's deckFormat code for Commander.
	commanderFormat = 3
	// minDeckSize filters out unfinished stubs from the search results.
	minDeckSize = 98
)

// minRequestInterval is the minimum gap between the starts of two Archidekt
// requests (SYN-003). A var so tests can shorten it.
var minRequestInterval = 6 * time.Second

// rateLimitBackoffs are the waits before each retry of a request Archidekt
// answered 429; Archidekt sends no Retry-After, so the schedule is ours. One
// more 429 after the last back-off stops the run (SYN-004). A var so tests can
// shorten it.
var rateLimitBackoffs = []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute}

// requestTimeout bounds one request — response headers and body decode — so a
// stalled connection cannot hang the worker. A deck detail is a few hundred
// KB. A var so tests can shorten it.
var requestTimeout = 60 * time.Second

// now is the clock the pacing reads. A var so tests can drive a fake clock.
var now = time.Now

// sleepCtx waits d or until ctx is done. A var so tests can observe the pacing
// and back-off waits without spending them.
var sleepCtx = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// errBudgetExhausted means the running job's request cap is spent (SYN-034).
// The job fails; nothing it had not finished writing is kept.
var errBudgetExhausted = errors.New("archidekt request cap for this job exhausted")

// statusError is a non-2xx, non-429 Archidekt answer.
type statusError struct {
	status int
	url    string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("archidekt: GET %s: HTTP %d", e.url, e.status)
}

// searchResult is one deck in an Archidekt search page; only the fields the
// candidate filter reads.
type searchResult struct {
	ID         int64 `json:"id"`
	Size       int   `json:"size"`
	DeckFormat int   `json:"deckFormat"`
	Private    bool  `json:"private"`
	Unlisted   bool  `json:"unlisted"`
}

type searchPage struct {
	Count   int            `json:"count"`
	Next    *string        `json:"next"`
	Results []searchResult `json:"results"`
}

// deckDetail is the part of an Archidekt deck the tally reads.
type deckDetail struct {
	ID         int64          `json:"id"`
	DeckFormat int            `json:"deckFormat"`
	Private    bool           `json:"private"`
	Unlisted   bool           `json:"unlisted"`
	Categories []deckCategory `json:"categories"`
	Cards      []deckCard     `json:"cards"`
}

type deckCategory struct {
	Name           string `json:"name"`
	IncludedInDeck bool   `json:"includedInDeck"`
}

type deckCard struct {
	Quantity   int      `json:"quantity"`
	Categories []string `json:"categories"`
	DeletedAt  *string  `json:"deletedAt"`
	Card       struct {
		OracleCard struct {
			UID        string   `json:"uid"` // = Scryfall oracle_id
			Name       string   `json:"name"`
			SuperTypes []string `json:"superTypes"`
		} `json:"oracleCard"`
	} `json:"card"`
}

// archidektClient reads Archidekt's two public deck endpoints (SYN-002) under
// the etiquette rules: an identifying User-Agent, at least
// minRequestInterval between request starts, and an escalating back-off on 429
// (SYN-003, SYN-004). The worker owns the only one, so the pace holds across
// every job. It counts every request it sends, retries included, against the
// current job's cap (SYN-034). It is used by one goroutine.
type archidektClient struct {
	baseURL     string
	http        *http.Client
	maxRequests int

	requests  int
	lastStart time.Time
}

func newArchidektClient(baseURL string, hc *http.Client, maxRequests int) *archidektClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if hc == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 30 * time.Second
		hc = &http.Client{Transport: transport}
	}
	return &archidektClient{baseURL: baseURL, http: hc, maxRequests: maxRequests}
}

// beginJob starts a new request budget for one job; the pacing clock carries
// over so the 6 s gap holds between jobs (SYN-003, SYN-034).
func (c *archidektClient) beginJob(maxRequests int) {
	c.requests = 0
	c.maxRequests = maxRequests
}

// search reads one page of a commander's most recent public Commander decks.
//
// @spec SYN-002
func (c *archidektClient) search(ctx context.Context, commanderName string, page int) (searchPage, error) {
	q := url.Values{}
	q.Set("deckFormat", strconv.Itoa(commanderFormat))
	q.Set("commanderName", commanderName)
	q.Set("orderBy", "-createdAt")
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	var out searchPage
	err := c.get(ctx, "/api/decks/v3/?"+q.Encode(), &out)
	return out, err
}

// deck reads one public deck. found is false when Archidekt answers 403, 404,
// or 410 — the deck went private or was deleted after the search listed it —
// which the caller skips (SYN-005).
//
// @spec SYN-002, SYN-005
func (c *archidektClient) deck(ctx context.Context, id int64) (d deckDetail, found bool, err error) {
	err = c.get(ctx, "/api/decks/"+strconv.FormatInt(id, 10)+"/", &d)
	var se *statusError
	if errors.As(err, &se) {
		switch se.status {
		case http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			return deckDetail{}, false, nil
		}
	}
	if err != nil {
		return deckDetail{}, false, err
	}
	return d, true, nil
}

// get performs one paced GET and decodes a 2xx JSON body into out. A 429 is
// retried after each rateLimitBackoffs wait in turn; one more 429 is an error.
//
// @spec SYN-003, SYN-004, SYN-034
func (c *archidektClient) get(ctx context.Context, path string, out any) error {
	full := c.baseURL + path
	for attempt := 0; ; attempt++ {
		if c.requests >= c.maxRequests {
			return errBudgetExhausted
		}
		if !c.lastStart.IsZero() {
			if wait := minRequestInterval - now().Sub(c.lastStart); wait > 0 {
				if err := sleepCtx(ctx, wait); err != nil {
					return err
				}
			}
		}
		c.lastStart = now()
		c.requests++

		status, err := c.do(ctx, full, out)
		if err != nil {
			return err
		}
		if status != http.StatusTooManyRequests {
			return nil
		}
		if attempt >= len(rateLimitBackoffs) {
			return fmt.Errorf("archidekt: GET %s: HTTP 429 persisted through %d back-offs", full, len(rateLimitBackoffs))
		}
		if err := sleepCtx(ctx, rateLimitBackoffs[attempt]); err != nil {
			return err
		}
	}
}

// do sends one request under requestTimeout. It returns 429 as a status for
// get to retry, decodes a 2xx body into out, and turns anything else into a
// *statusError.
func (c *archidektClient) do(ctx context.Context, full string, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", acceptHeader)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, &statusError{status: resp.StatusCode, url: full}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("archidekt: decode %s: %w", full, err)
	}
	return resp.StatusCode, nil
}
