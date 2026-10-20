package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/synergy"
)

func synergyRouter(a *API) http.Handler {
	r := chi.NewRouter()
	a.RegisterSynergyRoutes(r)
	return r
}

func synergyDo(t *testing.T, a *API, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	synergyRouter(a).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func synergyGet(t *testing.T, a *API, path string) *httptest.ResponseRecorder {
	return synergyDo(t, a, http.MethodGet, path)
}

// withSynergy gives the API an enabled synergy service whose worker never runs
// and whose Archidekt URL is unreachable: the endpoints only read and queue.
func withSynergy(t *testing.T, a *API, enabled bool) {
	t.Helper()
	a.Synergy = synergy.NewService(a.Pool, synergy.Options{
		Enabled: enabled, BaseURL: "http://127.0.0.1:1", MaxNewPerDay: 100000,
		Logf: func(string, ...any) {},
	})
}

func cleanupSynergy(t *testing.T, a *API, oracle pgtype.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = a.Pool.Exec(ctx, `DELETE FROM synergy_jobs WHERE commander_oracle_id = $1`, oracle)
		_, _ = a.Pool.Exec(ctx, `DELETE FROM synergy_commanders WHERE commander_oracle_id = $1`, oracle)
	})
}

// cardWithOracle upserts a commander-legal mirror card and returns its cards.id
// and oracle id.
func cardWithOracle(t *testing.T, a *API, name string, commander bool, legal string) (id, oracle pgtype.UUID) {
	t.Helper()
	oracle = randUUID(t)
	c, err := a.Queries.UpsertCard(context.Background(), db.UpsertCardParams{
		ScryfallOracleID:       oracle,
		Name:                   name,
		ManaValue:              2,
		TypeLine:               "Creature",
		Colors:                 []string{"R"},
		ColorIdentity:          []string{"R"},
		ProducedMana:           []string{},
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"` + legal + `"}`),
		Layout:                 "normal",
		CanBeCommander:         commander,
		CommanderColorIdentity: []string{"R"},
	})
	if err != nil {
		t.Fatalf("upsert card %q: %v", name, err)
	}
	return c.ID, oracle
}

// @spec SYN-040, SYN-041
func TestGetCommanderSynergy_ServesOrderedLegalCardsWithSampleAndAttribution(t *testing.T) {
	a := testAPI(t)
	withSynergy(t, a, true)
	ctx := context.Background()

	cmdID, cmdOracle := cardWithOracle(t, a, "API Synergy Commander", true, "legal")
	_, strong := cardWithOracle(t, a, "API Synergy Strong", false, "legal")
	_, staple := cardWithOracle(t, a, "API Synergy Staple", false, "legal")
	_, banned := cardWithOracle(t, a, "API Synergy Banned", false, "banned")
	unknown := randUUID(t) // an oracle id the mirror has never seen
	cleanupSynergy(t, a, cmdOracle)

	fetched := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Second), Valid: true}
	if err := a.Queries.UpsertSynergyCommander(ctx, db.UpsertSynergyCommanderParams{
		CommanderOracleID: cmdOracle, CommanderName: "API Synergy Commander", ColorIdentity: "R",
		DeckCount: 40, DuplicatesSkipped: 2, FetchedAt: fetched,
	}); err != nil {
		t.Fatal(err)
	}
	type row struct {
		oracle pgtype.UUID
		decks  int32
		rate   float64
		base   float64
	}
	var rows []db.InsertSynergyCommanderCardsParams
	for _, r := range []row{{strong, 36, 0.9, 0.1}, {staple, 40, 1.0, 1.0}, {banned, 38, 0.95, 0.0}, {unknown, 39, 0.97, 0.0}} {
		rows = append(rows, db.InsertSynergyCommanderCardsParams{
			CommanderOracleID: cmdOracle, CardOracleID: r.oracle, DeckCount: r.decks, InclusionRate: r.rate,
		})
	}
	if _, err := a.Queries.InsertSynergyCommanderCards(ctx, rows); err != nil {
		t.Fatal(err)
	}
	// Synergy needs a baseline: 0.1 for the strong card, 1.0 for the staple,
	// 0 for the rest.
	if _, err := a.Pool.Exec(ctx, `
		UPDATE synergy_commander_cards SET baseline_rate = CASE card_oracle_id WHEN $2 THEN 0.1 WHEN $3 THEN 1.0 ELSE 0 END
		WHERE commander_oracle_id = $1`, cmdOracle, strong, staple); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pool.Exec(ctx, `UPDATE synergy_commander_cards SET synergy = inclusion_rate - baseline_rate WHERE commander_oracle_id = $1`, cmdOracle); err != nil {
		t.Fatal(err)
	}

	rec := synergyGet(t, a, "/synergy/commanders/"+uuidString(cmdID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body commanderSynergyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.Status != "ready" || body.Reason != nil {
		t.Errorf("status = %q reason %v, want ready with no reason", body.Status, body.Reason)
	}
	if body.Sample == nil {
		t.Fatal("sample is null for a stored sample")
	}
	if body.Commander.Name != "API Synergy Commander" || body.Commander.CardID != uuidString(cmdID) || body.Commander.ColorIdentity != "R" {
		t.Errorf("commander = %+v", body.Commander)
	}
	if body.Sample.DeckCount != 40 || body.Sample.DuplicatesSkipped != 2 || !body.Sample.FetchedAt.Equal(fetched.Time) {
		t.Errorf("sample = %+v", body.Sample)
	}
	if body.Sample.BaselineDeckCount != nil || body.Sample.BaselineFetchedAt != nil {
		t.Errorf("baseline metadata = %v / %v, want null with no baseline sample", body.Sample.BaselineDeckCount, body.Sample.BaselineFetchedAt)
	}
	if body.Attribution["text"] != "Synergy data from public Archidekt decks" || body.Attribution["url"] != "https://archidekt.com" {
		t.Errorf("attribution = %v", body.Attribution)
	}

	// Banned and unknown-to-the-mirror cards never surface; the rest are
	// ordered by synergy descending.
	if len(body.Cards) != 2 || body.Cards[0].Card.Name != "API Synergy Strong" || body.Cards[1].Card.Name != "API Synergy Staple" {
		t.Fatalf("cards = %+v", body.Cards)
	}
	top := body.Cards[0]
	if top.Decks != 36 || top.Inclusion != 0.9 || top.Baseline == nil || *top.Baseline != 0.1 || top.Synergy == nil || *top.Synergy < 0.79 || *top.Synergy > 0.81 {
		t.Errorf("top card = %+v", top)
	}

	// limit is honoured and clamped.
	rec = synergyGet(t, a, "/synergy/commanders/"+uuidString(cmdID)+"?limit=1")
	var limited commanderSynergyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &limited)
	if len(limited.Cards) != 1 {
		t.Errorf("limit=1 returned %d cards", len(limited.Cards))
	}
	if rec := synergyGet(t, a, "/synergy/commanders/"+uuidString(cmdID)+"?limit=100000"); rec.Code != http.StatusOK {
		t.Errorf("an oversized limit = %d, want it clamped", rec.Code)
	}
}

// @spec SYN-040, SYN-042
func TestGetCommanderSynergy_NoSampleIsNoneWithEmptyCards_UnknownIs404_BadIDIs400(t *testing.T) {
	a := testAPI(t)
	withSynergy(t, a, true)
	noSample, oracle := cardWithOracle(t, a, "API Synergy No Sample", true, "legal")
	cleanupSynergy(t, a, oracle)

	rec := synergyGet(t, a, "/synergy/commanders/"+uuidString(noSample))
	if rec.Code != http.StatusOK {
		t.Fatalf("a commander with no sample = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body commanderSynergyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "none" || body.Reason != nil || body.Sample != nil || len(body.Cards) != 0 {
		t.Errorf("body = %+v, want none, no reason, no sample, no cards", body)
	}
	if body.Attribution["url"] != "https://archidekt.com" {
		t.Errorf("attribution = %v", body.Attribution)
	}
	if !strings.Contains(rec.Body.String(), `"cards":[]`) {
		t.Errorf("cards must serialise as [] not null: %s", rec.Body.String())
	}

	if rec := synergyGet(t, a, "/synergy/commanders/"+uuidString(randUUID(t))); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown card id = %d, want 404", rec.Code)
	}
	if rec := synergyGet(t, a, "/synergy/commanders/not-a-uuid"); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed id = %d, want 400", rec.Code)
	}
}

// @spec SYN-030, SYN-031
func TestEnsureCommanderSynergy_QueuesOnceAndStatusFollows(t *testing.T) {
	a := testAPI(t)
	withSynergy(t, a, true)
	cmdID, oracle := cardWithOracle(t, a, "API Synergy Ensure", true, "legal")
	cleanupSynergy(t, a, oracle)

	for i := 0; i < 2; i++ {
		rec := synergyDo(t, a, http.MethodPost, "/synergy/commanders/"+uuidString(cmdID)+"/ensure")
		if rec.Code != http.StatusOK {
			t.Fatalf("ensure #%d = %d: %s", i+1, rec.Code, rec.Body.String())
		}
		var st synergyStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		if st.Status != "gathering" || st.Reason != nil || st.Sample != nil {
			t.Errorf("ensure #%d = %+v, want gathering", i+1, st)
		}
	}
	var n int
	if err := a.Pool.QueryRow(context.Background(), `SELECT count(*) FROM synergy_jobs WHERE commander_oracle_id = $1`, oracle).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d queued jobs (%v), want exactly 1", n, err)
	}

	var body commanderSynergyResponse
	rec := synergyGet(t, a, "/synergy/commanders/"+uuidString(cmdID))
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Status != "gathering" {
		t.Errorf("GET status after ensure = %q, want gathering", body.Status)
	}
}

// @spec SYN-001, SYN-030, SYN-042
func TestEnsureCommanderSynergy_DisabledNotACommanderUnknownAndUnconfigured(t *testing.T) {
	a := testAPI(t)
	cmdID, cmdOracle := cardWithOracle(t, a, "API Synergy Disabled", true, "legal")
	plainID, plainOracle := cardWithOracle(t, a, "API Synergy Plain Card", false, "legal")
	cleanupSynergy(t, a, cmdOracle)
	cleanupSynergy(t, a, plainOracle)
	post := func(id pgtype.UUID) (*httptest.ResponseRecorder, synergyStatusResponse) {
		rec := synergyDo(t, a, http.MethodPost, "/synergy/commanders/"+uuidString(id)+"/ensure")
		var st synergyStatusResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &st)
		return rec, st
	}

	// No service wired at all.
	if rec, _ := post(cmdID); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured = %d, want 503", rec.Code)
	}

	withSynergy(t, a, false)
	if rec, st := post(cmdID); rec.Code != http.StatusOK || st.Status != "none" || st.Reason == nil || *st.Reason != "disabled" {
		t.Errorf("disabled = %d %+v, want none/disabled", rec.Code, st)
	}

	withSynergy(t, a, true)
	if rec, st := post(plainID); rec.Code != http.StatusOK || st.Status != "none" || st.Reason == nil || *st.Reason != "not_a_commander" {
		t.Errorf("non-commander = %d %+v, want none/not_a_commander", rec.Code, st)
	}
	if rec, _ := post(randUUID(t)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown card = %d, want 404", rec.Code)
	}
	if rec := synergyDo(t, a, http.MethodPost, "/synergy/commanders/nope/ensure"); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id = %d, want 400", rec.Code)
	}
}
