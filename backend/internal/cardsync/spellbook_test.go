package cardsync_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"manafold-backend/internal/cardsync"
	db "manafold-backend/internal/db/generated"
)

// spellbookPool connects to the test database and holds an advisory lock for
// the test: the combo pass replaces whole tables, so combo-dependent tests in
// other packages take the same lock (key 7241) to stay out of its way.
func spellbookPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7241)`); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(7241)`)
		conn.Release()
		pool.Close()
	})
	return pool
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

const (
	oracleA = "11111111-1111-4111-8111-111111111111"
	oracleB = "22222222-2222-4222-8222-222222222222"
	oracleC = "33333333-3333-4333-8333-333333333333"
)

func variantJSON(id, status string, commander bool, requires string, uses ...string) string {
	var us []string
	for _, oracle := range uses {
		us = append(us, fmt.Sprintf(`{"card":{"name":"x","oracleId":%q},"zoneLocations":["H"],"mustBeCommander":false,"quantity":1}`, oracle))
	}
	return fmt.Sprintf(`{"id":%q,"status":%q,"uses":[%s],"requires":%s,
		"produces":[{"feature":{"name":"Win the game","status":"S"},"quantity":1}],
		"identity":"UB","manaNeeded":"{U}","manaValueNeeded":1,"notablePrerequisites":"","bracketTag":"R",
		"popularity":7,"legalities":{"commander":%v}}`, id, status, strings.Join(us, ","), requires, commander)
}

func writeVariants(t *testing.T, variants ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "variants.json")
	body := `{"timestamp":"2026-10-19T19:13:21.926562+00:00","version":"7.2.0","variants":[` +
		strings.Join(variants, ",") + `],"aliases":[{"id":1,"x":[1,2,{"y":3}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCards(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cards.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunSpellbook_DevSeed loads the checked-in combo seed and checks the
// stored rows, then re-runs it for idempotence.
//
// @spec CARD-050, CARD-051, CARD-055
func TestRunSpellbook_DevSeed(t *testing.T) {
	pool := spellbookPool(t)
	ctx := context.Background()
	opts := cardsync.SpellbookOptions{
		VariantsPath: "../../seed/spellbook-variants.json",
		CardsPath:    "../../seed/spellbook-cards.json",
	}
	for run := 1; run <= 2; run++ {
		res, err := cardsync.RunSpellbook(ctx, pool, opts)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if res.Combos != 3 || res.CardFlags != 2 {
			t.Fatalf("run %d result = %+v, want 3 combos and 2 flagged cards", run, res)
		}
		if n := count(t, pool, "combos"); n != 3 {
			t.Fatalf("run %d: combos = %d, want 3", run, n)
		}
		if n := count(t, pool, "combo_cards"); n != 6 {
			t.Fatalf("run %d: combo_cards = %d, want 6", run, n)
		}
		if n := count(t, pool, "spellbook_card_flags"); n != 2 {
			t.Fatalf("run %d: spellbook_card_flags = %d, want 2", run, n)
		}
	}

	var tag, mana string
	var manaValue int
	var notable bool
	var produces []byte
	err := pool.QueryRow(ctx,
		`SELECT bracket_tag, mana_needed, mana_value_needed, has_notable_prerequisites, produces FROM combos WHERE id = '742-1295'`).
		Scan(&tag, &mana, &manaValue, &notable, &produces)
	if err != nil {
		t.Fatalf("read combo: %v", err)
	}
	if tag != "R" || manaValue != 3 || mana != "{U}{U}{B}" {
		t.Fatalf("combo = %q %q %d, want R {U}{U}{B} 3", tag, mana, manaValue)
	}
	var feats []map[string]string
	if err := json.Unmarshal(produces, &feats); err != nil || len(feats) < 2 {
		t.Fatalf("produces = %s (%v), want its features", produces, err)
	}

	run, err := db.New(pool).LatestSyncRun(ctx, "spellbook_variants")
	if err != nil || run.Status != "succeeded" || run.RowsUpserted != 3 || !run.ScryfallUpdatedAt.Valid {
		t.Fatalf("variants sync run = %+v (%v), want succeeded / 3 rows / export timestamp", run, err)
	}
}

// TestRunSpellbook_Eligibility checks which variants are mirrored, that a card
// repeated within a variant is summed, and that an export with no eligible
// variant leaves the previous mirror in place.
//
// @spec CARD-050, CARD-051
func TestRunSpellbook_Eligibility(t *testing.T) {
	pool := spellbookPool(t)
	ctx := context.Background()
	cards := writeCards(t, `{"results":[{"oracleId":"`+oracleA+`","name":"A","massLandDenial":true,"extraTurn":false}]}`)

	good := writeVariants(t,
		variantJSON("ok-1", "OK", true, "[]", oracleA, oracleB),
		variantJSON("example-1", "E", true, "[]", oracleA, oracleB, oracleB),
		variantJSON("not-legal", "OK", false, "[]", oracleA, oracleB),
		variantJSON("draft", "D", true, "[]", oracleA, oracleB),
		variantJSON("template", "OK", true, `[{"template":{"id":1}}]`, oracleA, oracleB),
		variantJSON("bad-oracle", "OK", true, "[]", oracleA, "not-a-uuid"),
		variantJSON("no-cards", "OK", true, "[]"),
		variantJSON("ok-1", "OK", true, "[]", oracleA, oracleC), // duplicate id: first wins
	)
	res, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{VariantsPath: good, CardsPath: cards})
	if err != nil {
		t.Fatalf("RunSpellbook: %v", err)
	}
	if res.Combos != 2 || res.TemplateSkipped != 1 || res.Ineligible != 4 {
		t.Fatalf("result = %+v, want 2 combos, 1 template skipped, 4 ineligible", res)
	}
	var q int
	if err := pool.QueryRow(ctx, `SELECT quantity FROM combo_cards WHERE combo_id = 'example-1' AND scryfall_oracle_id = $1`, oracleB).Scan(&q); err != nil || q != 2 {
		t.Fatalf("repeated ingredient quantity = %d (%v), want 2", q, err)
	}
	if n := count(t, pool, "combo_cards"); n != 4 {
		t.Fatalf("combo_cards = %d, want 4 (ok-1 first wins: A,B; example-1: A,B)", n)
	}

	// An export with nothing eligible fails and keeps the previous combos.
	empty := writeVariants(t, variantJSON("x", "OK", false, "[]", oracleA))
	_, err = cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{VariantsPath: empty, CardsPath: cards})
	if err == nil {
		t.Fatal("RunSpellbook over an export with no eligible variant succeeded; want an error")
	}
	if n := count(t, pool, "combos"); n != 2 {
		t.Fatalf("combos after a failed pass = %d, want the previous 2", n)
	}
	run, err := db.New(pool).LatestSyncRun(ctx, "spellbook_variants")
	if err != nil || run.Status != "failed" || !run.Error.Valid {
		t.Fatalf("sync run = %+v (%v), want failed with error text", run, err)
	}

	// A truncated export fails the same way.
	trunc := filepath.Join(t.TempDir(), "trunc.json")
	if err := os.WriteFile(trunc, []byte(`{"variants":[`+variantJSON("t", "OK", true, "[]", oracleA)+`,{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{VariantsPath: trunc, CardsPath: cards}); err == nil {
		t.Fatal("RunSpellbook over a truncated export succeeded; want an error")
	}
	if n := count(t, pool, "combos"); n != 2 {
		t.Fatalf("combos after a truncated export = %d, want the previous 2", n)
	}
}

// TestRunSpellbook_DownloadsVariants serves the export over HTTP and checks the
// etiquette headers, that the temp spool is removed, and the size cap.
//
// @spec CARD-052
func TestRunSpellbook_DownloadsVariants(t *testing.T) {
	pool := spellbookPool(t)
	ctx := context.Background()
	cards := writeCards(t, `{"results":[{"oracleId":"`+oracleA+`","name":"A","extraTurn":true}]}`)

	body, err := os.ReadFile(writeVariants(t, variantJSON("dl-1", "OK", true, "[]", oracleA, oracleB)))
	if err != nil {
		t.Fatal(err)
	}
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	spool := t.TempDir()
	restore := *cardsync.TempFileDir
	*cardsync.TempFileDir = spool
	t.Cleanup(func() { *cardsync.TempFileDir = restore })

	res, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsURL: srv.URL, HTTPClient: srv.Client(), CardsPath: cards,
	})
	if err != nil || res.Combos != 1 {
		t.Fatalf("RunSpellbook = %+v, %v; want 1 combo", res, err)
	}
	if !strings.HasPrefix(gotUA, "Manafold/") {
		t.Fatalf("User-Agent = %q, want the Manafold etiquette header", gotUA)
	}
	if entries, _ := os.ReadDir(spool); len(entries) != 0 {
		t.Fatalf("%d spool file(s) left behind after success", len(entries))
	}

	// Over the size cap: fails, spool removed, previous mirror kept.
	restoreMax := *cardsync.SpellbookMaxVariantsBytes
	*cardsync.SpellbookMaxVariantsBytes = 64
	t.Cleanup(func() { *cardsync.SpellbookMaxVariantsBytes = restoreMax })
	if _, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsURL: srv.URL, HTTPClient: srv.Client(), CardsPath: cards,
	}); err == nil {
		t.Fatal("download over the size cap succeeded; want an error")
	}
	if entries, _ := os.ReadDir(spool); len(entries) != 0 {
		t.Fatalf("%d spool file(s) left behind after failure", len(entries))
	}
	if n := count(t, pool, "combos"); n != 1 {
		t.Fatalf("combos = %d, want the previous 1", n)
	}
}

// TestRunSpellbook_CardFlagCrawl checks paging, the same-host rule, the page
// cap, the empty-crawl guard, and the weekly skip.
//
// @spec CARD-053, CARD-054
func TestRunSpellbook_CardFlagCrawl(t *testing.T) {
	pool := spellbookPool(t)
	ctx := context.Background()
	variants := writeVariants(t, variantJSON("c-1", "OK", true, "[]", oracleA, oracleB))
	// Earlier tests recorded succeeded card-flag runs; start without any.
	if _, err := pool.Exec(ctx, `DELETE FROM sync_runs WHERE bulk_type = 'spellbook_cards'`); err != nil {
		t.Fatal(err)
	}

	restoreDelay := *cardsync.SpellbookPageDelay
	*cardsync.SpellbookPageDelay = time.Millisecond
	t.Cleanup(func() { *cardsync.SpellbookPageDelay = restoreDelay })

	page := func(next string, results ...string) string {
		n := "null"
		if next != "" {
			n = fmt.Sprintf("%q", next)
		}
		return fmt.Sprintf(`{"next":%s,"results":[%s]}`, n, strings.Join(results, ","))
	}
	card := func(oracle string, mld, et bool) string {
		return fmt.Sprintf(`{"oracleId":%q,"name":"n-%s","massLandDenial":%v,"extraTurn":%v}`, oracle, oracle[:2], mld, et)
	}

	var srv *httptest.Server
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/cards/", func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Query().Get("offset") {
		case "":
			_, _ = w.Write([]byte(page(srv.URL+"/cards/?offset=1", card(oracleA, true, false), card(oracleB, false, false))))
		default:
			_, _ = w.Write([]byte(page("", card(oracleC, false, true), card(oracleA, false, true))))
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsPath: variants, CardsURL: srv.URL + "/cards/", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("crawl: %v", err)
	}
	if res.CardFlags != 2 || hits != 2 {
		t.Fatalf("result = %+v after %d page request(s), want 2 flagged cards over 2 pages", res, hits)
	}
	var mld, et bool
	if err := pool.QueryRow(ctx, `SELECT mass_land_denial, extra_turn FROM spellbook_card_flags WHERE scryfall_oracle_id = $1`, oracleA).Scan(&mld, &et); err != nil || !mld || !et {
		t.Fatalf("flags for the card seen twice = %v/%v (%v), want both set", mld, et, err)
	}

	// Fresh enough: the second run skips the crawl and makes no request.
	res, err = cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsPath: variants, CardsURL: srv.URL + "/cards/", HTTPClient: srv.Client(),
	})
	if err != nil || !res.CardsSkipped || hits != 2 {
		t.Fatalf("second run = %+v, %v after %d hits, want the crawl skipped", res, err, hits)
	}
	if _, err := pool.Exec(ctx, `UPDATE sync_runs SET started_at = now() - interval '8 days' WHERE bulk_type = 'spellbook_cards'`); err != nil {
		t.Fatal(err)
	}

	// A next link off the start host is refused; the table is untouched.
	off := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page("http://example.invalid/cards/?offset=1", card(oracleB, true, true))))
	}))
	t.Cleanup(off.Close)
	if _, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsPath: variants, CardsURL: off.URL + "/cards/", HTTPClient: off.Client(),
	}); err == nil {
		t.Fatal("crawl followed a next link off the start host; want an error")
	}
	if n := count(t, pool, "spellbook_card_flags"); n != 2 {
		t.Fatalf("flags after a refused crawl = %d, want the previous 2", n)
	}

	// An endless chain stops at the page cap.
	restorePages := *cardsync.SpellbookMaxPages
	*cardsync.SpellbookMaxPages = 3
	t.Cleanup(func() { *cardsync.SpellbookMaxPages = restorePages })
	var loop *httptest.Server
	loop = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page(loop.URL+"/cards/?x=1", card(oracleB, true, false))))
	}))
	t.Cleanup(loop.Close)
	if _, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsPath: variants, CardsURL: loop.URL + "/cards/", HTTPClient: loop.Client(),
	}); err == nil {
		t.Fatal("endless crawl succeeded; want the page cap to fail it")
	}

	// A crawl that sees no cards at all fails without touching the table.
	none := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page("")))
	}))
	t.Cleanup(none.Close)
	if _, err := cardsync.RunSpellbook(ctx, pool, cardsync.SpellbookOptions{
		VariantsPath: variants, CardsURL: none.URL + "/cards/", HTTPClient: none.Client(),
	}); err == nil {
		t.Fatal("empty crawl succeeded; want an error")
	}
	if n := count(t, pool, "spellbook_card_flags"); n != 2 {
		t.Fatalf("flags after an empty crawl = %d, want the previous 2", n)
	}
}
