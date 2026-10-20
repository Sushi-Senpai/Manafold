package cardsync

// The Commander Spellbook combo pass: RunSpellbook mirrors Spellbook's daily
// variants.json export into combos / combo_cards (CARD-050..052) and its
// mass-land-denial / extra-turn card flags into spellbook_card_flags
// (CARD-053, CARD-054). It is the only code that contacts Commander Spellbook
// (CARD-056). The classification of those combos lives in internal/bracket.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

const (
	defaultSpellbookVariantsURL = "https://json.commanderspellbook.com/variants.json"
	defaultSpellbookCardsURL    = "https://backend.commanderspellbook.com/cards/?limit=100"

	bulkTypeSpellbookVariants = "spellbook_variants"
	bulkTypeSpellbookCards    = "spellbook_cards"
)

// spellbookMaxVariantsBytes caps the variants.json download (CARD-052): the
// export is ~700 MB, so a response several times that is runaway. A var so
// tests can shrink it.
var spellbookMaxVariantsBytes int64 = 4 << 30

// spellbookRequestTimeout bounds each /cards/ page request (CARD-053).
var spellbookRequestTimeout = time.Minute

// spellbookPageDelay is the pause between /cards/ page requests (CARD-053).
var spellbookPageDelay = 500 * time.Millisecond

// spellbookMaxPages caps the /cards/ crawl (CARD-053); ~330 pages exist today.
var spellbookMaxPages = 1000

// spellbookCardsRefresh is how long a succeeded card-flag crawl is reused
// (CARD-054).
var spellbookCardsRefresh = 7 * 24 * time.Hour

// SpellbookOptions configures the combo pass. With VariantsPath / CardsPath set
// the pass reads those local files instead of downloading (CARD-055).
type SpellbookOptions struct {
	VariantsURL string
	CardsURL    string
	HTTPClient  *http.Client
	// VariantsPath is a variants.json-shaped file: {timestamp, variants: [...]}.
	VariantsPath string
	// CardsPath is one /cards/-shaped page: {results: [...]}.
	CardsPath string
}

// SpellbookResult reports what the combo pass did.
type SpellbookResult struct {
	Combos          int
	TemplateSkipped int  // variants needing a template ingredient
	Ineligible      int  // other variants not stored (status, legality, ingredients)
	CardFlags       int  // cards flagged mass land denial or extra turn
	CardsSkipped    bool // the card-flag crawl was fresh enough to skip
}

// RunSpellbook runs the variants pass, then the card-flag pass, each under its
// own sync_runs row. A failure in one does not stop the other; the returned
// error joins whatever failed.
//
// @spec CARD-050, CARD-051, CARD-053, CARD-054, CARD-055
func RunSpellbook(ctx context.Context, pool *pgxpool.Pool, opts SpellbookOptions) (SpellbookResult, error) {
	client := opts.HTTPClient
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		client = &http.Client{Transport: transport}
	}
	f := &fetcher{client: client}
	q := db.New(pool)

	var res SpellbookResult
	var errs []error

	if err := runVariants(ctx, pool, q, f, opts, &res); err != nil {
		errs = append(errs, fmt.Errorf("spellbook variants: %w", err))
	}
	if err := runCardFlags(ctx, pool, q, f, opts, &res); err != nil {
		errs = append(errs, fmt.Errorf("spellbook cards: %w", err))
	}
	return res, errors.Join(errs...)
}

// ---- variants ------------------------------------------------------------

// spellbookVariant is the slice of a variants.json element the mirror reads.
// Everything else — descriptions, images, prices — is skipped by the decoder.
type spellbookVariant struct {
	ID   string `json:"id"`
	Uses []struct {
		Card struct {
			OracleID string `json:"oracleId"`
		} `json:"card"`
		ZoneLocations   []string `json:"zoneLocations"`
		MustBeCommander bool     `json:"mustBeCommander"`
		Quantity        int      `json:"quantity"`
	} `json:"uses"`
	Requires []json.RawMessage `json:"requires"`
	Produces []struct {
		Feature struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"feature"`
	} `json:"produces"`
	Status               string `json:"status"`
	Identity             string `json:"identity"`
	ManaNeeded           string `json:"manaNeeded"`
	ManaValueNeeded      int    `json:"manaValueNeeded"`
	NotablePrerequisites string `json:"notablePrerequisites"`
	BracketTag           string `json:"bracketTag"`
	Popularity           *int32 `json:"popularity"`
	Legalities           struct {
		Commander bool `json:"commander"`
	} `json:"legalities"`
}

type comboFeature struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type comboIngredient struct {
	OracleID        string `json:"oracle_id"`
	Quantity        int    `json:"quantity"`
	ZoneLocations   string `json:"zone_locations"`
	MustBeCommander bool   `json:"must_be_commander"`
}

type eligibility int

const (
	eligible eligibility = iota
	skippedTemplate
	skippedOther
)

// classifyVariant decides whether a variant is mirrored (CARD-050) and, if so,
// returns its distinct ingredients: a card repeated within one variant has its
// quantities summed, its zones unioned, and its commander flag or-ed.
func classifyVariant(v spellbookVariant) (eligibility, []comboIngredient) {
	if v.Status != "OK" && v.Status != "E" || !v.Legalities.Commander || len(v.Uses) == 0 || v.ID == "" {
		return skippedOther, nil
	}
	if len(v.Requires) > 0 {
		return skippedTemplate, nil
	}
	byCard := map[string]*comboIngredient{}
	var order []string
	for _, u := range v.Uses {
		oracle, ok := parseUUID(u.Card.OracleID)
		if !ok {
			return skippedOther, nil
		}
		key := uuidString(oracle)
		q := u.Quantity
		if q < 1 {
			q = 1
		}
		zones := strings.Join(u.ZoneLocations, "")
		if ing, seen := byCard[key]; seen {
			ing.Quantity += q
			for _, z := range zones {
				if !strings.ContainsRune(ing.ZoneLocations, z) {
					ing.ZoneLocations += string(z)
				}
			}
			ing.MustBeCommander = ing.MustBeCommander || u.MustBeCommander
			continue
		}
		byCard[key] = &comboIngredient{OracleID: key, Quantity: q, ZoneLocations: zones, MustBeCommander: u.MustBeCommander}
		order = append(order, key)
	}
	out := make([]comboIngredient, 0, len(order))
	for _, k := range order {
		out = append(out, *byCard[k])
	}
	return eligible, out
}

func uuidString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

const comboImportDDL = `CREATE TEMP TABLE combo_import (
	id                        text NOT NULL,
	identity                  text NOT NULL,
	mana_needed               text NOT NULL,
	mana_value_needed         integer NOT NULL,
	has_notable_prerequisites boolean NOT NULL,
	produces                  jsonb NOT NULL,
	bracket_tag               text NOT NULL,
	popularity                integer,
	ingredients               jsonb NOT NULL
) ON COMMIT DROP`

var comboImportColumns = []string{
	"id", "identity", "mana_needed", "mana_value_needed", "has_notable_prerequisites",
	"produces", "bracket_tag", "popularity", "ingredients",
}

const comboMergeSQL = `
INSERT INTO combos (id, identity, mana_needed, mana_value_needed, has_notable_prerequisites, produces, bracket_tag, popularity)
SELECT id, identity, mana_needed, mana_value_needed, has_notable_prerequisites, produces, bracket_tag, popularity
FROM combo_import`

const comboCardsMergeSQL = `
INSERT INTO combo_cards (combo_id, scryfall_oracle_id, quantity, zone_locations, must_be_commander)
SELECT i.id, r.oracle_id, r.quantity, r.zone_locations, r.must_be_commander
FROM combo_import i,
     jsonb_to_recordset(i.ingredients) AS r(oracle_id uuid, quantity integer, zone_locations text, must_be_commander boolean)`

// runVariants ingests variants.json under a spellbook_variants sync_runs row.
//
// @spec CARD-050, CARD-051, CARD-052, CARD-055
func runVariants(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, f *fetcher, opts SpellbookOptions, res *SpellbookResult) error {
	var path string
	if opts.VariantsPath != "" {
		path = opts.VariantsPath
	} else {
		u := opts.VariantsURL
		if u == "" {
			u = defaultSpellbookVariantsURL
		}
		dl, err := f.downloadFile(ctx, u, "spellbook-variants-*.json", spellbookMaxVariantsBytes)
		if err != nil {
			return err
		}
		defer os.Remove(dl)
		path = dl
	}

	run, err := q.CreateSyncRun(ctx, db.CreateSyncRunParams{BulkType: bulkTypeSpellbookVariants})
	if err != nil {
		return err
	}
	stored, exportTime, ingestErr := ingestVariants(ctx, pool, path, res)
	if ingestErr != nil {
		_ = q.FailSyncRun(ctx, db.FailSyncRunParams{ID: run.ID, Error: text(ingestErr.Error())})
		return ingestErr
	}
	res.Combos = stored
	return q.FinishSyncRunWithSource(ctx, db.FinishSyncRunWithSourceParams{
		ID: run.ID, RowsUpserted: int32(stored), ScryfallUpdatedAt: tstz(exportTime),
	})
}

// ingestVariants streams the file's variants into a staging table and replaces
// the mirror from it in one transaction (CARD-051). The previous mirror is
// untouched on any error or when no variant is eligible.
func ingestVariants(ctx context.Context, pool *pgxpool.Pool, path string, res *SpellbookResult) (int, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer file.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, comboImportDDL); err != nil {
		return 0, time.Time{}, fmt.Errorf("create staging table: %w", err)
	}

	dec := json.NewDecoder(file)
	if err := expectDelim(dec, '{'); err != nil {
		return 0, time.Time{}, fmt.Errorf("variants export: %w", err)
	}
	var exportTime time.Time
	if err := seekVariants(dec, &exportTime); err != nil {
		return 0, time.Time{}, fmt.Errorf("variants export: %w", err)
	}

	var templates, other int
	seen := map[string]struct{}{}
	staged, copyErr := tx.CopyFrom(ctx, pgx.Identifier{"combo_import"}, comboImportColumns,
		pgx.CopyFromFunc(func() ([]any, error) {
			for dec.More() {
				var v spellbookVariant
				if err := dec.Decode(&v); err != nil {
					return nil, fmt.Errorf("decode variant: %w", err)
				}
				kind, ings := classifyVariant(v)
				switch kind {
				case skippedTemplate:
					templates++
					continue
				case skippedOther:
					other++
					continue
				}
				if _, dup := seen[v.ID]; dup {
					continue
				}
				seen[v.ID] = struct{}{}
				return comboRow(v, ings), nil
			}
			return nil, nil
		}))
	if copyErr != nil {
		return 0, time.Time{}, fmt.Errorf("stage combos: %w", copyErr)
	}
	if err := expectDelim(dec, ']'); err != nil {
		return 0, time.Time{}, fmt.Errorf("variants export: %w", err)
	}
	if err := finishExport(dec, &exportTime); err != nil {
		return 0, time.Time{}, fmt.Errorf("variants export: %w", err)
	}
	if staged == 0 {
		return 0, time.Time{}, errors.New("export holds no eligible commander variant; keeping the previous combos")
	}

	if _, err := tx.Exec(ctx, `DELETE FROM combos`); err != nil {
		return 0, time.Time{}, fmt.Errorf("clear combos: %w", err)
	}
	if _, err := tx.Exec(ctx, comboMergeSQL); err != nil {
		return 0, time.Time{}, fmt.Errorf("merge combos: %w", err)
	}
	if _, err := tx.Exec(ctx, comboCardsMergeSQL); err != nil {
		return 0, time.Time{}, fmt.Errorf("merge combo cards: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, time.Time{}, err
	}
	res.TemplateSkipped = templates
	res.Ineligible = other
	return int(staged), exportTime, nil
}

// comboRow is one combo_import COPY row, in comboImportColumns order.
func comboRow(v spellbookVariant, ings []comboIngredient) []any {
	features := make([]comboFeature, 0, len(v.Produces))
	for _, p := range v.Produces {
		features = append(features, comboFeature{Name: p.Feature.Name, Status: p.Feature.Status})
	}
	producesJSON, _ := json.Marshal(features)
	ingredientsJSON, _ := json.Marshal(ings)
	var popularity any
	if v.Popularity != nil {
		popularity = *v.Popularity
	}
	return []any{
		v.ID, v.Identity, v.ManaNeeded, int32(v.ManaValueNeeded),
		strings.TrimSpace(v.NotablePrerequisites) != "",
		producesJSON, v.BracketTag, popularity, ingredientsJSON,
	}
}

// seekVariants reads the export's top-level keys until it has consumed the
// opening bracket of "variants", recording "timestamp" on the way and skipping
// other keys without holding them in memory.
func seekVariants(dec *json.Decoder, exportTime *time.Time) error {
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return err
		}
		if key == "variants" {
			return expectDelim(dec, '[')
		}
		if err := readTopLevelValue(dec, key, exportTime); err != nil {
			return err
		}
	}
	return errors.New(`no "variants" array`)
}

// finishExport consumes the keys that follow the variants array.
func finishExport(dec *json.Decoder, exportTime *time.Time) error {
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return err
		}
		if err := readTopLevelValue(dec, key, exportTime); err != nil {
			return err
		}
	}
	return expectDelim(dec, '}')
}

func readKey(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	key, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("expected an object key, got %v", tok)
	}
	return key, nil
}

func readTopLevelValue(dec *json.Decoder, key string, exportTime *time.Time) error {
	if key != "timestamp" {
		return skipValue(dec)
	}
	var ts string
	if err := dec.Decode(&ts); err != nil {
		return err
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		*exportTime = t
	}
	return nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, got %v", want, tok)
	}
	return nil
}

// skipValue consumes one JSON value token by token, so an unread top-level key
// (aliases) is never held whole in memory.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || (d != '{' && d != '[') {
		return nil
	}
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			default:
				depth--
			}
		}
	}
	return nil
}

// ---- card flags ----------------------------------------------------------

type spellbookCardsPage struct {
	Next    string `json:"next"`
	Results []struct {
		OracleID       string `json:"oracleId"`
		Name           string `json:"name"`
		MassLandDenial bool   `json:"massLandDenial"`
		ExtraTurn      bool   `json:"extraTurn"`
	} `json:"results"`
}

type cardFlag struct {
	oracleID pgtype.UUID
	name     string
	mld, et  bool
}

// runCardFlags refreshes spellbook_card_flags under a spellbook_cards sync_runs
// row, skipping a crawl while the last success is under a week old (CARD-054).
//
// @spec CARD-053, CARD-054, CARD-055
func runCardFlags(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, f *fetcher, opts SpellbookOptions, res *SpellbookResult) error {
	if opts.CardsPath == "" {
		last, err := q.LatestSucceededSyncRun(ctx, bulkTypeSpellbookCards)
		if err == nil && last.StartedAt.Valid && time.Since(last.StartedAt.Time) < spellbookCardsRefresh {
			res.CardsSkipped = true
			return nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}

	run, err := q.CreateSyncRun(ctx, db.CreateSyncRunParams{BulkType: bulkTypeSpellbookCards})
	if err != nil {
		return err
	}
	fail := func(err error) error {
		_ = q.FailSyncRun(ctx, db.FailSyncRunParams{ID: run.ID, Error: text(err.Error())})
		return err
	}

	var flags []cardFlag
	var seenCards int
	collect := func(p spellbookCardsPage) {
		for _, c := range p.Results {
			seenCards++
			if !c.MassLandDenial && !c.ExtraTurn {
				continue
			}
			oracle, ok := parseUUID(c.OracleID)
			if !ok {
				continue
			}
			flags = append(flags, cardFlag{oracleID: oracle, name: c.Name, mld: c.MassLandDenial, et: c.ExtraTurn})
		}
	}

	if opts.CardsPath != "" {
		raw, err := os.ReadFile(opts.CardsPath)
		if err != nil {
			return fail(err)
		}
		var page spellbookCardsPage
		if err := json.Unmarshal(raw, &page); err != nil {
			return fail(fmt.Errorf("decode cards page: %w", err))
		}
		collect(page)
	} else {
		start := opts.CardsURL
		if start == "" {
			start = defaultSpellbookCardsURL
		}
		if err := crawlCards(ctx, f, start, collect); err != nil {
			return fail(err)
		}
	}
	if seenCards == 0 {
		return fail(errors.New("crawl returned no cards; keeping the previous card flags"))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback(ctx)
	tq := q.WithTx(tx)
	if err := tq.DeleteAllSpellbookCardFlags(ctx); err != nil {
		return fail(err)
	}
	for _, c := range flags {
		if err := tq.InsertSpellbookCardFlag(ctx, db.InsertSpellbookCardFlagParams{
			ScryfallOracleID: c.oracleID, CardName: c.name, MassLandDenial: c.mld, ExtraTurn: c.et,
		}); err != nil {
			return fail(err)
		}
	}
	var distinct int32
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM spellbook_card_flags`).Scan(&distinct); err != nil {
		return fail(err)
	}
	if err := tq.FinishSyncRun(ctx, db.FinishSyncRunParams{ID: run.ID, RowsUpserted: distinct}); err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(err)
	}
	res.CardFlags = int(distinct)
	return nil
}

// crawlCards pages through Spellbook's /cards/ API, following each page's
// `next` link only while it stays on the start URL's host (CARD-053).
func crawlCards(ctx context.Context, f *fetcher, start string, collect func(spellbookCardsPage)) error {
	startURL, err := url.Parse(start)
	if err != nil {
		return err
	}
	next := start
	for pages := 0; next != ""; pages++ {
		if pages >= spellbookMaxPages {
			return fmt.Errorf("card crawl hit the %d-page cap before the last page", spellbookMaxPages)
		}
		if pages > 0 {
			select {
			case <-time.After(spellbookPageDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		page, err := fetchCardsPage(ctx, f, next)
		if err != nil {
			return err
		}
		collect(page)

		next = ""
		if page.Next != "" {
			nu, err := url.Parse(page.Next)
			if err != nil || nu.Host != startURL.Host {
				return fmt.Errorf("card crawl: refusing to follow next link %q off %s", page.Next, startURL.Host)
			}
			next = page.Next
		}
	}
	return nil
}

func fetchCardsPage(ctx context.Context, f *fetcher, pageURL string) (spellbookCardsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, spellbookRequestTimeout)
	defer cancel()
	body, err := f.get(ctx, pageURL)
	if err != nil {
		return spellbookCardsPage{}, err
	}
	defer body.Close()
	var page spellbookCardsPage
	if err := json.NewDecoder(body).Decode(&page); err != nil && !errors.Is(err, io.EOF) {
		return spellbookCardsPage{}, fmt.Errorf("decode cards page: %w", err)
	}
	return page, nil
}
