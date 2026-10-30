package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/ai"
	db "manafold-backend/internal/db/generated"
)

// ---- fixtures -----------------------------------------------------------

type wizCardSpec struct {
	name     string
	typeLine string
	text     string
	identity []string
	produced []string
	gc       bool
	legal    string // commander legality; default "legal"
	usd      *float64
	manaCost string
}

type wizCard struct {
	ID     pgtype.UUID
	Oracle pgtype.UUID
	Name   string
}

// wizFixture owns one wizard test's rows and removes them afterwards, so the
// edhrec_rank-ordered pools other wizard tests read are not crowded by leftovers.
type wizFixture struct {
	t      *testing.T
	a      *API
	owner  pgtype.UUID
	suffix string

	cardIDs  []pgtype.UUID
	deckIDs  []string
	tagIDs   []pgtype.UUID
	comboIDs []string
}

func newWizFixture(t *testing.T) *wizFixture {
	t.Helper()
	a := testAPI(t)
	a.AIWizardDailyLimit = 40
	fx := &wizFixture{t: t, a: a, owner: makeUser(t, a), suffix: hex.EncodeToString(randBytes(t, 4))}
	t.Cleanup(fx.cleanup)
	return fx
}

func (fx *wizFixture) cleanup() {
	ctx := context.Background()
	for _, id := range fx.deckIDs {
		_, _ = fx.a.Pool.Exec(ctx, `DELETE FROM decks WHERE id = $1`, id)
	}
	_, _ = fx.a.Pool.Exec(ctx, `DELETE FROM ai_usage WHERE user_id = $1`, fx.owner)
	for _, id := range fx.comboIDs {
		_, _ = fx.a.Pool.Exec(ctx, `DELETE FROM combos WHERE id = $1`, id)
	}
	for _, id := range fx.tagIDs {
		_, _ = fx.a.Pool.Exec(ctx, `DELETE FROM oracle_tags WHERE id = $1`, id)
	}
	for _, id := range fx.cardIDs {
		_, _ = fx.a.Pool.Exec(ctx, `DELETE FROM cards WHERE id = $1`, id)
	}
}

// card upserts a mirror card with edhrec_rank 0 (so it leads every rank-ordered
// pool) and an optional USD price.
func (fx *wizFixture) card(s wizCardSpec) wizCard {
	fx.t.Helper()
	if s.typeLine == "" {
		s.typeLine = "Creature — Test"
	}
	if s.legal == "" {
		s.legal = "legal"
	}
	if s.identity == nil {
		s.identity = []string{}
	}
	if s.produced == nil {
		s.produced = []string{}
	}
	name := fmt.Sprintf("%s %s", s.name, fx.suffix)
	oracle := randUUID(fx.t)
	c, err := fx.a.Queries.UpsertCard(context.Background(), db.UpsertCardParams{
		ScryfallOracleID:       oracle,
		Name:                   name,
		ManaCost:               pgtype.Text{String: s.manaCost, Valid: s.manaCost != ""},
		ManaValue:              2,
		TypeLine:               s.typeLine,
		OracleText:             s.text,
		Colors:                 s.identity,
		ColorIdentity:          s.identity,
		ProducedMana:           s.produced,
		Keywords:               []string{},
		Legalities:             json.RawMessage(`{"commander":"` + s.legal + `"}`),
		IsGameChanger:          s.gc,
		Layout:                 "normal",
		CommanderColorIdentity: s.identity,
		EdhrecRank:             pgtype.Int4{Int32: 0, Valid: true},
	})
	if err != nil {
		fx.t.Fatalf("upsert card %q: %v", name, err)
	}
	fx.cardIDs = append(fx.cardIDs, c.ID)
	if s.usd != nil {
		fx.price(c.ID, *s.usd)
	}
	return wizCard{ID: c.ID, Oracle: oracle, Name: name}
}

func (fx *wizFixture) price(card pgtype.UUID, usd float64) {
	fx.t.Helper()
	if _, err := fx.a.Pool.Exec(context.Background(),
		`INSERT INTO card_prints (scryfall_id, card_id, set_code, prices) VALUES ($1, $2, 'tst', $3::jsonb)`,
		randUUID(fx.t), card, fmt.Sprintf(`{"usd":"%.2f"}`, usd)); err != nil {
		fx.t.Fatalf("insert price: %v", err)
	}
}

func f64(v float64) *float64 { return &v }

// deck creates an owned deck with a commander of the given identity. bracket 0
// leaves it unset.
func (fx *wizFixture) deck(identity []string, bracket int, archetype string) (deckID string, commander wizCard) {
	fx.t.Helper()
	commander = fx.card(wizCardSpec{name: "Wiz Commander", typeLine: "Legendary Creature — Test", identity: identity})
	if _, err := fx.a.Pool.Exec(context.Background(), `UPDATE cards SET can_be_commander = true WHERE id = $1`, commander.ID); err != nil {
		fx.t.Fatal(err)
	}
	deckID = fx.emptyDeck()
	rec := serve(fx.t, fx.a, fx.owner, http.MethodPut, "/decks/"+deckID+"/commander", map[string]string{"commander_card_id": uuidString(commander.ID)})
	if rec.Code != http.StatusOK {
		fx.t.Fatalf("set commander: %d %s", rec.Code, rec.Body.String())
	}
	fx.patch(deckID, bracket, archetype)
	return deckID, commander
}

func (fx *wizFixture) emptyDeck() string {
	fx.t.Helper()
	rec := serve(fx.t, fx.a, fx.owner, http.MethodPost, "/decks", map[string]string{"name": "Wizard Test Deck"})
	if rec.Code != http.StatusCreated {
		fx.t.Fatalf("create deck: %d %s", rec.Code, rec.Body.String())
	}
	id := decode[deckJSON](fx.t, rec).ID
	fx.deckIDs = append(fx.deckIDs, id)
	return id
}

func (fx *wizFixture) patch(deckID string, bracket int, archetype string) {
	fx.t.Helper()
	body := map[string]any{}
	if bracket > 0 {
		body["bracket"] = bracket
	}
	if archetype != "" {
		body["archetype"] = archetype
	}
	if len(body) == 0 {
		return
	}
	if rec := serve(fx.t, fx.a, fx.owner, http.MethodPatch, "/decks/"+deckID, body); rec.Code != http.StatusOK {
		fx.t.Fatalf("patch deck: %d %s", rec.Code, rec.Body.String())
	}
}

func (fx *wizFixture) add(deckID string, c wizCard, board string, qty int) {
	fx.t.Helper()
	id, ok := parseUUID(deckID)
	if !ok {
		fx.t.Fatalf("bad deck id %q", deckID)
	}
	if _, err := fx.a.Queries.AddDeckCard(context.Background(), db.AddDeckCardParams{
		CardID: c.ID, Quantity: int32(qty), Board: board, DeckID: id, UserID: fx.owner,
	}); err != nil {
		fx.t.Fatalf("add %s to deck: %v", c.Name, err)
	}
}

// tag creates an oracle tag with the given slug (optionally under a parent tag)
// and returns its id. Slugs are not unique in the mirror, so this coexists with
// a seeded tag of the same slug.
func (fx *wizFixture) tag(slug string, parent *pgtype.UUID) pgtype.UUID {
	fx.t.Helper()
	id := randUUID(fx.t)
	parents := []pgtype.UUID{}
	if parent != nil {
		parents = append(parents, *parent)
	}
	if _, err := fx.a.Pool.Exec(context.Background(),
		`INSERT INTO oracle_tags (id, slug, label, parent_ids) VALUES ($1, $2, $2, $3)`, id, slug, parents); err != nil {
		fx.t.Fatalf("insert tag %q: %v", slug, err)
	}
	fx.tagIDs = append(fx.tagIDs, id)
	return id
}

func (fx *wizFixture) tagCard(tag pgtype.UUID, c wizCard) {
	fx.t.Helper()
	if _, err := fx.a.Pool.Exec(context.Background(),
		`INSERT INTO card_oracle_tags (card_id, tag_id, weight) VALUES ($1, $2, 'median')`, c.ID, tag); err != nil {
		fx.t.Fatalf("tag card: %v", err)
	}
}

// combo records a Commander Spellbook combo over the given cards.
func (fx *wizFixture) combo(manaValue int, cards ...wizCard) {
	fx.t.Helper()
	id := "wiz-" + fx.suffix + "-" + fmt.Sprint(len(fx.comboIDs))
	if _, err := fx.a.Pool.Exec(context.Background(),
		`INSERT INTO combos (id, identity, mana_value_needed, has_notable_prerequisites, produces, bracket_tag)
		 VALUES ($1, 'W', $2, false, '[{"name":"Infinite mana","status":"S"}]', 'R')`, id, manaValue); err != nil {
		fx.t.Fatalf("insert combo: %v", err)
	}
	fx.comboIDs = append(fx.comboIDs, id)
	for _, c := range cards {
		if _, err := fx.a.Pool.Exec(context.Background(),
			`INSERT INTO combo_cards (combo_id, scryfall_oracle_id, quantity, zone_locations, must_be_commander)
			 VALUES ($1, $2, 1, 'H', false)`, id, c.Oracle); err != nil {
			fx.t.Fatalf("insert combo card: %v", err)
		}
	}
}

func (fx *wizFixture) usage(feature string) (calls int64) {
	fx.t.Helper()
	if err := fx.a.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(calls), 0) FROM ai_usage WHERE user_id = $1 AND feature = $2`, fx.owner, feature).Scan(&calls); err != nil {
		fx.t.Fatal(err)
	}
	return calls
}

// ---- driving the endpoints ----------------------------------------------

func (fx *wizFixture) get(deckID string) wizardPlanResponse {
	fx.t.Helper()
	rec := aiServe(fx.t, fx.a, fx.owner, "", http.MethodGet, "/decks/"+deckID+"/wizard", nil)
	if rec.Code != http.StatusOK {
		fx.t.Fatalf("plan: %d %s", rec.Code, rec.Body.String())
	}
	return decode[wizardPlanResponse](fx.t, rec)
}

func (fx *wizFixture) proposeRaw(deckID string, body any) *httptest.ResponseRecorder {
	fx.t.Helper()
	return aiServe(fx.t, fx.a, fx.owner, "", http.MethodPost, "/decks/"+deckID+"/wizard/propose", body)
}

func (fx *wizFixture) propose(deckID string, body any) wizardProposeResponse {
	fx.t.Helper()
	rec := fx.proposeRaw(deckID, body)
	if rec.Code != http.StatusOK {
		fx.t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
	}
	return decode[wizardProposeResponse](fx.t, rec)
}

// assistant installs a scripted Curate; the returned counter reports how many
// calls reached it.
func (fx *wizFixture) assistant(curate func(ai.CurateRequest) (ai.CurateResult, error)) *int {
	calls := 0
	fx.a.AI = fakeAssistant{enabled: true, curate: func(r ai.CurateRequest) (ai.CurateResult, error) {
		calls++
		return curate(r)
	}}
	return &calls
}

var testUsage = ai.Usage{Model: "claude-sonnet-5", InputTokens: 100, OutputTokens: 50, CostMicros: 1}

// pickNames makes Curate answer with the named cards.
func pickNames(names ...string) func(ai.CurateRequest) (ai.CurateResult, error) {
	return func(ai.CurateRequest) (ai.CurateResult, error) {
		res := ai.CurateResult{Summary: "Here are my picks.", Usage: testUsage}
		for _, n := range names {
			res.Picks = append(res.Picks, ai.Suggestion{Name: n, Reason: "fits " + n})
		}
		return res, nil
	}
}

// capture records the request Curate saw and then picks the first pool card.
func capture(into *ai.CurateRequest) func(ai.CurateRequest) (ai.CurateResult, error) {
	return func(r ai.CurateRequest) (ai.CurateResult, error) {
		*into = r
		res := ai.CurateResult{Summary: "ok", Usage: testUsage}
		if len(r.Pool) > 0 {
			res.Picks = []ai.Suggestion{{Name: r.Pool[0].Name, Reason: "first"}}
		}
		return res, nil
	}
}

func shouldNotBeCalled(t *testing.T) func(ai.CurateRequest) (ai.CurateResult, error) {
	return func(ai.CurateRequest) (ai.CurateResult, error) {
		t.Error("Curate was called, but this request must not reach the model")
		return ai.CurateResult{}, errors.New("unexpected call")
	}
}

func poolNames(p []ai.PoolCard) map[string]bool {
	out := map[string]bool{}
	for _, c := range p {
		out[c.Name] = true
	}
	return out
}

func proposalNames(p []wizardProposal) []string {
	var out []string
	for _, x := range p {
		out = append(out, x.Card.Name)
	}
	return out
}

func wantIn(t *testing.T, pool map[string]bool, label string, c wizCard) {
	t.Helper()
	if !pool[c.Name] {
		t.Errorf("%s: pool is missing %q", label, c.Name)
	}
}

func wantOut(t *testing.T, pool map[string]bool, label string, c wizCard) {
	t.Helper()
	if pool[c.Name] {
		t.Errorf("%s: pool wrongly contains %q", label, c.Name)
	}
}

// ---- plan ---------------------------------------------------------------

// @spec WIZ-002
func TestWizardPlan_ReadinessReasonsInOrder(t *testing.T) {
	fx := newWizFixture(t)

	noCommander := fx.emptyDeck()
	p := fx.get(noCommander)
	if p.Ready || p.Reason == nil || *p.Reason != "no_commander" {
		t.Errorf("no commander: ready=%v reason=%v", p.Ready, p.Reason)
	}

	noBracket, _ := fx.deck([]string{"W"}, 0, "")
	if p := fx.get(noBracket); p.Ready || p.Reason == nil || *p.Reason != "no_bracket" {
		t.Errorf("no bracket: ready=%v reason=%v", p.Ready, p.Reason)
	}

	five, _ := fx.deck([]string{"W"}, 5, "")
	p = fx.get(five)
	if p.Ready || p.Reason == nil || *p.Reason != "bracket_unsupported" || p.Policy != nil {
		t.Errorf("bracket 5: ready=%v reason=%v policy=%v", p.Ready, p.Reason, p.Policy)
	}

	ok, _ := fx.deck([]string{"W"}, 3, "")
	p = fx.get(ok)
	if !p.Ready || p.Reason != nil || p.Bracket == nil || *p.Bracket != 3 || p.Policy == nil || p.Policy.Guidance == "" {
		t.Errorf("ready deck: %+v", p)
	}
}

// @spec WIZ-002, WIZ-003
func TestWizardPlan_PolicyAndLandNumbers(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W", "U"}, 3, "midrange_value")

	g1 := fx.card(wizCardSpec{name: "Plan GC One", typeLine: "Artifact", gc: true})
	g2 := fx.card(wizCardSpec{name: "Plan GC Two", typeLine: "Artifact", gc: true})
	plain := fx.card(wizCardSpec{name: "Plan Plain", typeLine: "Artifact"})
	land := fx.card(wizCardSpec{name: "Plan Land", typeLine: "Land", identity: []string{"W"}, produced: []string{"W"}})
	maybeLand := fx.card(wizCardSpec{name: "Plan Maybe Land", typeLine: "Land"})
	fx.add(deckID, g1, "main", 1)
	fx.add(deckID, g2, "main", 1)
	fx.add(deckID, plain, "main", 1)
	fx.add(deckID, land, "main", 5)
	fx.add(deckID, maybeLand, "maybe", 4) // staging board: not counted

	p := fx.get(deckID)
	if p.Archetype == nil || *p.Archetype != "midrange_value" {
		t.Errorf("archetype = %v", p.Archetype)
	}
	if p.Policy.GameChangerLimit == nil || *p.Policy.GameChangerLimit != 3 || p.Policy.GameChangersInDeck != 2 ||
		p.Policy.GameChangersLeft == nil || *p.Policy.GameChangersLeft != 1 {
		t.Errorf("policy = %+v", p.Policy)
	}
	if p.Lands.Target != 37 || p.Lands.InDeck != 5 || p.Lands.BasicsNeeded != 32 || !p.Lands.FixingApplicable {
		t.Errorf("lands = %+v, want target 37, 5 in deck, 32 basics, fixing applicable", p.Lands)
	}

	mono, _ := fx.deck([]string{"W"}, 4, "")
	pm := fx.get(mono)
	if pm.Lands.FixingApplicable || pm.Policy.GameChangerLimit != nil || pm.Policy.GameChangersLeft != nil || pm.Lands.Target != 37 {
		t.Errorf("mono bracket 4: %+v / %+v", pm.Policy, pm.Lands)
	}
}

// @spec WIZ-002, WIZ-004
func TestWizardPlan_OwnerOnlyAndNeedsNoAI(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "")
	fx.a.AI = ai.Disabled()

	if p := fx.get(deckID); !p.Ready || p.AIEnabled {
		t.Errorf("AI disabled: ready=%v ai_enabled=%v, want ready with ai_enabled false", p.Ready, p.AIEnabled)
	}

	stranger := makeUser(t, fx.a)
	if rec := aiServe(t, fx.a, stranger, "", http.MethodGet, "/decks/"+deckID+"/wizard", nil); rec.Code != http.StatusNotFound {
		t.Errorf("non-owner plan = %d, want 404", rec.Code)
	}
	if rec := aiServe(t, fx.a, stranger, "", http.MethodPost, "/decks/"+deckID+"/wizard/basics", map[string]any{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-owner basics = %d, want 404", rec.Code)
	}

	// An anonymous draft's owner can read the plan too.
	rec := aiServe(t, fx.a, pgtype.UUID{}, "wiz-anon-"+fx.suffix, http.MethodPost, "/decks", map[string]string{"name": "Anon Wizard Deck"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("anon create: %d %s", rec.Code, rec.Body.String())
	}
	anonDeck := decode[deckJSON](t, rec).ID
	fx.deckIDs = append(fx.deckIDs, anonDeck)
	if rec := aiServe(t, fx.a, pgtype.UUID{}, "wiz-anon-"+fx.suffix, http.MethodGet, "/decks/"+anonDeck+"/wizard", nil); rec.Code != http.StatusOK {
		t.Errorf("anon plan = %d, want 200", rec.Code)
	}
}

// @spec WIZ-005
func TestWizardPlan_EnsuresSynergyForReadyDeck(t *testing.T) {
	fx := newWizFixture(t)
	withSynergy(t, fx.a, true)
	deckID, cmd := fx.deck([]string{"W"}, 3, "")
	cleanupSynergy(t, fx.a, cmd.Oracle)

	p := fx.get(deckID)
	if p.Synergy == nil || p.Synergy.Status != "gathering" {
		t.Fatalf("synergy = %+v, want gathering", p.Synergy)
	}
	var n int
	if err := fx.a.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM synergy_jobs WHERE commander_oracle_id = $1`, cmd.Oracle).Scan(&n); err != nil || n != 1 {
		t.Errorf("synergy jobs = %d (%v), want the plan to have queued one", n, err)
	}

	// A deck that is not ready asks for nothing.
	noBracket, cmd2 := fx.deck([]string{"W"}, 0, "")
	cleanupSynergy(t, fx.a, cmd2.Oracle)
	if p := fx.get(noBracket); p.Synergy != nil {
		t.Errorf("not-ready deck reported synergy %+v", p.Synergy)
	}
	if err := fx.a.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM synergy_jobs WHERE commander_oracle_id = $1`, cmd2.Oracle).Scan(&n); err != nil || n != 0 {
		t.Errorf("jobs for a not-ready deck = %d, want 0", n)
	}
}

// ---- propose: gates -----------------------------------------------------

// @spec WIZ-010
func TestWizardPropose_GateOrder(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "")
	fx.card(wizCardSpec{name: "Gate Order Pool Card", identity: []string{"W"}}) // a non-empty pool, so a call reaches the model
	body := map[string]any{"step": "picks"}
	path := "/decks/" + deckID + "/wizard/propose"

	// 503: assistant disabled — before anything else, even for a stranger.
	fx.a.AI = ai.Disabled()
	if rec := aiServe(t, fx.a, fx.owner, "", http.MethodPost, path, body); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled = %d, want 503", rec.Code)
	}

	calls := fx.assistant(pickNames())

	// 403: anonymous draft.
	if rec := aiServe(t, fx.a, pgtype.UUID{}, "wiz-anon-"+fx.suffix, http.MethodPost, path, body); rec.Code != http.StatusForbidden {
		t.Errorf("anonymous = %d, want 403", rec.Code)
	}

	// 404: a non-owner learns nothing about quota or spend, even at the ceiling.
	fx.a.AIMonthlySpendMicros = 1
	stranger := makeUser(t, fx.a)
	t.Cleanup(func() {
		_, _ = fx.a.Pool.Exec(context.Background(), `DELETE FROM ai_usage WHERE user_id = $1`, stranger)
	})
	if err := fx.a.Queries.RecordAIUsage(context.Background(), db.RecordAIUsageParams{UserID: fx.owner, Feature: "wizard", InputTokens: 1, OutputTokens: 1, CostMicros: 5}); err != nil {
		t.Fatal(err)
	}
	if rec := aiServe(t, fx.a, stranger, "", http.MethodPost, path, body); rec.Code != http.StatusNotFound {
		t.Errorf("non-owner at the ceiling = %d, want 404", rec.Code)
	}

	// 503: the global monthly ceiling.
	if rec := aiServe(t, fx.a, fx.owner, "", http.MethodPost, path, body); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("ceiling = %d, want 503", rec.Code)
	}

	// 429: the wizard's own daily limit, counted on the wizard feature only.
	fx.a.AIMonthlySpendMicros = 0
	fx.a.AIWizardDailyLimit = 1 // the usage row recorded above is that one call
	if rec := aiServe(t, fx.a, fx.owner, "", http.MethodPost, path, body); rec.Code != http.StatusTooManyRequests {
		t.Errorf("daily limit = %d, want 429", rec.Code)
	}
	fx.a.AIWizardDailyLimit = 2
	if rec := aiServe(t, fx.a, fx.owner, "", http.MethodPost, path, body); rec.Code != http.StatusOK {
		t.Errorf("under the limit = %d, want 200", rec.Code)
	}
	if *calls == 0 {
		t.Error("the call under the limit never reached the assistant")
	}
	// The suggest feature has its own allowance: the wizard's spend did not use it.
	if err := fx.a.Queries.RecordAIUsage(context.Background(), db.RecordAIUsageParams{UserID: fx.owner, Feature: "suggest", InputTokens: 1, OutputTokens: 1, CostMicros: 1}); err != nil {
		t.Fatal(err)
	}
	fx.a.AIWizardDailyLimit = 40
	if rec := aiServe(t, fx.a, fx.owner, "", http.MethodPost, path, body); rec.Code != http.StatusOK {
		t.Errorf("after a suggest call = %d, want 200", rec.Code)
	}
}

// @spec WIZ-011
func TestWizardPropose_NotReadyIs422WithoutAModelCall(t *testing.T) {
	fx := newWizFixture(t)
	fx.assistant(shouldNotBeCalled(t))

	noCommander := fx.emptyDeck()
	noBracket, _ := fx.deck([]string{"W"}, 0, "")
	five, _ := fx.deck([]string{"W"}, 5, "")
	for label, id := range map[string]string{"no commander": noCommander, "no bracket": noBracket, "bracket 5": five} {
		rec := fx.proposeRaw(id, map[string]any{"step": "picks"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s, want 422", label, rec.Code, rec.Body.String())
		}
	}
	if n := fx.usage("wizard"); n != 0 {
		t.Errorf("metered %d calls for requests that never reached the model", n)
	}
}

// @spec WIZ-012
func TestWizardPropose_RejectsMalformedBodies(t *testing.T) {
	fx := newWizFixture(t)
	fx.assistant(shouldNotBeCalled(t))
	deckID, _ := fx.deck([]string{"W", "U"}, 3, "")

	many := make([]string, 201)
	for i := range many {
		many[i] = uuidString(randUUID(t))
	}
	longNotes := make([]byte, 501)
	for i := range longNotes {
		longNotes[i] = 'x'
	}
	cases := map[string]map[string]any{
		"unknown step":      {"step": "lands"},
		"missing step":      {},
		"bad rejected id":   {"step": "picks", "rejected_card_ids": []string{"not-a-uuid"}},
		"too many rejected": {"step": "picks", "rejected_card_ids": many},
		"notes too long":    {"step": "picks", "restrictions": map[string]any{"notes": string(longNotes)}},
		"zero price cap":    {"step": "picks", "restrictions": map[string]any{"max_price_usd": 0}},
		"negative price":    {"step": "picks", "restrictions": map[string]any{"max_price_usd": -4}},
		"land target low":   {"step": "fixing", "land_target": 19},
		"land target high":  {"step": "fixing", "land_target": 61},
	}
	for label, body := range cases {
		if rec := fx.proposeRaw(deckID, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", label, rec.Code, rec.Body.String())
		}
	}
	if rec := aiServe(t, fx.a, fx.owner, "", http.MethodPost, "/decks/"+deckID+"/wizard/propose", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("empty body = %d, want 400", rec.Code)
	}
	if n := fx.usage("wizard"); n != 0 {
		t.Errorf("metered %d calls for rejected bodies", n)
	}
}

// @spec WIZ-013, WIZ-042
func TestWizardPropose_EmptyAnswersSkipTheModel(t *testing.T) {
	fx := newWizFixture(t)
	fx.assistant(shouldNotBeCalled(t))

	expect := func(label string, rec *httptest.ResponseRecorder, reason string) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", label, rec.Code, rec.Body.String())
		}
		got := decode[wizardProposeResponse](t, rec)
		if got.EmptyReason == nil || *got.EmptyReason != reason || len(got.Proposals) != 0 {
			t.Errorf("%s: empty_reason=%v proposals=%d, want %q and none", label, got.EmptyReason, len(got.Proposals), reason)
		}
	}

	mono, _ := fx.deck([]string{"W"}, 3, "")
	expect("mono-colour fixing", fx.proposeRaw(mono, map[string]any{"step": "fixing"}), "no_fixing_needed")

	pair, _ := fx.deck([]string{"W", "U"}, 3, "")
	basic := fx.card(wizCardSpec{name: "Wiz Plains", typeLine: "Basic Land — Plains", identity: []string{"W"}, produced: []string{"W"}})
	fx.add(pair, basic, "main", 20)
	expect("land target met (fixing)", fx.proposeRaw(pair, map[string]any{"step": "fixing", "land_target": 20}), "land_target_met")
	expect("land target met (utility)", fx.proposeRaw(pair, map[string]any{"step": "utility", "land_target": 20}), "land_target_met")

	full, _ := fx.deck([]string{"W"}, 3, "")
	fx.add(full, basic, "main", 100)
	expect("deck full", fx.proposeRaw(full, map[string]any{"step": "picks"}), "deck_full")

	open, _ := fx.deck([]string{"W"}, 3, "")
	expect("nothing under the price cap", fx.proposeRaw(open, map[string]any{
		"step": "picks", "restrictions": map[string]any{"max_price_usd": 0.0001},
	}), "no_candidates")

	if n := fx.usage("wizard"); n != 0 {
		t.Errorf("metered %d calls for answers that skipped the model", n)
	}
}

// @spec WIZ-014, WIZ-015
func TestWizardPropose_MetersEveryCallThatReachedTheModel(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "")
	good := fx.card(wizCardSpec{name: "Meter Good", typeLine: "Creature — Soldier", identity: []string{"W"}})

	fx.assistant(pickNames(good.Name))
	resp := fx.propose(deckID, map[string]any{"step": "picks"})
	if len(resp.Proposals) != 1 || resp.Proposals[0].Card.Name != good.Name || resp.Proposals[0].Reason != "fits "+good.Name {
		t.Fatalf("proposals = %+v", resp.Proposals)
	}
	if resp.Step != "picks" || resp.Summary != "Here are my picks." || resp.Model != "claude-sonnet-5" || resp.PoolSize == 0 || resp.EmptyReason != nil {
		t.Errorf("response = %+v", resp)
	}
	if n := fx.usage("wizard"); n != 1 {
		t.Errorf("successful call metered %d, want 1", n)
	}

	// A call that reached the model but came back unusable is still metered, then 502.
	fx.assistant(func(ai.CurateRequest) (ai.CurateResult, error) {
		return ai.CurateResult{Usage: testUsage}, errors.New("no submit_picks")
	})
	if rec := fx.proposeRaw(deckID, map[string]any{"step": "picks"}); rec.Code != http.StatusBadGateway {
		t.Errorf("unusable answer = %d, want 502", rec.Code)
	}
	if n := fx.usage("wizard"); n != 2 {
		t.Errorf("unusable answer left %d metered calls, want 2", n)
	}

	// A transport failure before any response carries no usage: not metered.
	fx.assistant(func(ai.CurateRequest) (ai.CurateResult, error) {
		return ai.CurateResult{}, errors.New("dial tcp: refused")
	})
	if rec := fx.proposeRaw(deckID, map[string]any{"step": "picks"}); rec.Code != http.StatusBadGateway {
		t.Errorf("transport failure = %d, want 502", rec.Code)
	}
	if n := fx.usage("wizard"); n != 2 {
		t.Errorf("transport failure changed the metered count to %d", n)
	}

	// The assistant reporting itself unconfigured is a 503.
	fx.assistant(func(ai.CurateRequest) (ai.CurateResult, error) { return ai.CurateResult{}, ai.ErrNotConfigured })
	if rec := fx.proposeRaw(deckID, map[string]any{"step": "picks"}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("not configured = %d, want 503", rec.Code)
	}
}

// @spec WIZ-015
func TestWizardPropose_CapsProposalsAtTheStepLimit(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W", "U"}, 3, "")
	var names []string
	for i := 0; i < 14; i++ {
		c := fx.card(wizCardSpec{name: fmt.Sprintf("Cap Dual %02d", i), typeLine: "Land", identity: []string{"W", "U"}, produced: []string{"W", "U"}})
		names = append(names, c.Name)
	}
	fx.assistant(pickNames(names...))
	if got := fx.propose(deckID, map[string]any{"step": "fixing", "include_fetch": false}); len(got.Proposals) != 10 {
		t.Errorf("fixing returned %d proposals, want at most 10", len(got.Proposals))
	}

	var lands []string
	for i := 0; i < 8; i++ {
		c := fx.card(wizCardSpec{name: fmt.Sprintf("Cap Util %02d", i), typeLine: "Land", identity: []string{}})
		fx.tagCard(fx.tag("utility-land", nil), c)
		lands = append(lands, c.Name)
	}
	fx.assistant(pickNames(lands...))
	if got := fx.propose(deckID, map[string]any{"step": "utility"}); len(got.Proposals) != 5 {
		t.Errorf("utility returned %d proposals, want at most 5", len(got.Proposals))
	}
}

// ---- propose: gate, pool membership, admission ---------------------------

// @spec WIZ-020
func TestWizardPropose_GateAndPoolMembershipDropWithoutSubstituting(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 4, "")
	tutors := fx.tag("tutor", nil)

	good := fx.card(wizCardSpec{name: "Gate Good", typeLine: "Creature — Soldier", identity: []string{"W"}})
	offColour := fx.card(wizCardSpec{name: "Gate Off Colour", identity: []string{"B"}})
	banned := fx.card(wizCardSpec{name: "Gate Banned", identity: []string{"W"}, legal: "banned"})
	onDeck := fx.card(wizCardSpec{name: "Gate On Deck", identity: []string{"W"}})
	tutor := fx.card(wizCardSpec{name: "Gate Tutor", identity: []string{"W"}})
	fx.tagCard(tutors, tutor)
	fx.add(deckID, onDeck, "main", 1)

	fx.assistant(pickNames(good.Name, "Totally Made Up Card "+fx.suffix, offColour.Name, banned.Name, onDeck.Name, good.Name, tutor.Name))
	got := fx.propose(deckID, map[string]any{"step": "picks", "restrictions": map[string]any{"no_tutors": true}})

	names := proposalNames(got.Proposals)
	if len(names) != 1 || names[0] != good.Name {
		t.Fatalf("proposals = %v, want only %q", names, good.Name)
	}
	// nonexistent, off-colour, banned, on-deck, a repeat of the good card, and the
	// tutor the restriction removed from the pool.
	if got.Dropped != 6 {
		t.Errorf("dropped = %d, want 6", got.Dropped)
	}
}

// @spec WIZ-033, WIZ-034, WIZ-030, WIZ-031, WIZ-032
func TestWizardPropose_PicksPoolRules(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "")
	tutorTag := fx.tag("tutor", nil)
	childTag := fx.tag("tutor-to-hand", &tutorTag)

	good := fx.card(wizCardSpec{name: "Rule Good", identity: []string{"W"}, usd: f64(2)})
	pricey := fx.card(wizCardSpec{name: "Rule Pricey", identity: []string{"W"}, usd: f64(50)})
	unpriced := fx.card(wizCardSpec{name: "Rule Unpriced", identity: []string{"W"}})
	tutor := fx.card(wizCardSpec{name: "Rule Tutor", identity: []string{"W"}, usd: f64(1)})
	fx.tagCard(childTag, tutor) // a descendant of the tutor tag counts
	offColour := fx.card(wizCardSpec{name: "Rule Off Colour", identity: []string{"U"}, usd: f64(1)})
	banned := fx.card(wizCardSpec{name: "Rule Banned", identity: []string{"W"}, legal: "banned", usd: f64(1)})
	land := fx.card(wizCardSpec{name: "Rule Land", typeLine: "Land", identity: []string{"W"}, produced: []string{"W"}, usd: f64(1)})
	onDeck := fx.card(wizCardSpec{name: "Rule On Deck", identity: []string{"W"}, usd: f64(1)})
	rejected := fx.card(wizCardSpec{name: "Rule Rejected", identity: []string{"W"}, usd: f64(1)})
	fx.add(deckID, onDeck, "maybe", 1) // any board counts as "in the deck"

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))

	fx.propose(deckID, map[string]any{
		"step":              "picks",
		"rejected_card_ids": []string{uuidString(rejected.ID)},
		"restrictions":      map[string]any{"notes": "  play lots of\nsoldiers  "},
	})
	pool := poolNames(seen.Pool)
	for label, c := range map[string]wizCard{"good": good, "pricey": pricey, "unpriced": unpriced, "tutor": tutor} {
		wantIn(t, pool, "unrestricted/"+label, c)
	}
	for label, c := range map[string]wizCard{"off-colour": offColour, "banned": banned, "land": land, "on deck (maybe)": onDeck, "rejected": rejected} {
		wantOut(t, pool, "unrestricted/"+label, c)
	}
	if seen.Notes != "play lots of soldiers" {
		t.Errorf("notes passed to the model = %q", seen.Notes)
	}
	if seen.Bracket != 2 || seen.BracketGuidance == "" || seen.GameChangersLeft == nil || *seen.GameChangersLeft != 0 || seen.CommanderName == "" || seen.Want != 10 {
		t.Errorf("request context = bracket %d, game changers left %v, want %d", seen.Bracket, seen.GameChangersLeft, seen.Want)
	}

	fx.propose(deckID, map[string]any{"step": "picks", "restrictions": map[string]any{"no_tutors": true}})
	pool = poolNames(seen.Pool)
	wantIn(t, pool, "no_tutors", good)
	wantOut(t, pool, "no_tutors", tutor)

	fx.propose(deckID, map[string]any{"step": "picks", "restrictions": map[string]any{"max_price_usd": 5}})
	pool = poolNames(seen.Pool)
	wantIn(t, pool, "price cap", good)
	wantIn(t, pool, "price cap", tutor)
	wantOut(t, pool, "price cap", pricey)
	wantOut(t, pool, "price cap", unpriced)

	// A price is the lowest among printings.
	fx.price(pricey.ID, 3)
	fx.propose(deckID, map[string]any{"step": "picks", "restrictions": map[string]any{"max_price_usd": 5}})
	wantIn(t, poolNames(seen.Pool), "cheapest printing", pricey)
	for _, c := range seen.Pool {
		if c.Name == pricey.Name && (c.PriceUSD == nil || *c.PriceUSD != 3) {
			t.Errorf("price shown for the card = %v, want its cheapest printing 3", c.PriceUSD)
		}
	}
}

// @spec WIZ-021, WIZ-023, WIZ-044
func TestWizardPropose_GameChangerBudget(t *testing.T) {
	fx := newWizFixture(t)
	mk := func(n string) wizCard {
		return fx.card(wizCardSpec{name: n, typeLine: "Artifact", identity: []string{"W"}, gc: true})
	}
	g1, g2, g3 := mk("Budget GC One"), mk("Budget GC Two"), mk("Budget GC Three")
	control := fx.card(wizCardSpec{name: "Budget Control", identity: []string{"W"}})

	var seen ai.CurateRequest

	// Bracket 2: no Game Changer is offered at all.
	b2, _ := fx.deck([]string{"W"}, 2, "")
	fx.assistant(capture(&seen))
	fx.propose(b2, map[string]any{"step": "picks"})
	pool := poolNames(seen.Pool)
	wantIn(t, pool, "bracket 2", control)
	for _, g := range []wizCard{g1, g2, g3} {
		wantOut(t, pool, "bracket 2", g)
	}

	// Bracket 3 with two already in the deck: one more is admissible, and only one
	// of three Game Changers the model names survives.
	held1, held2 := mk("Budget Held One"), mk("Budget Held Two")
	b3, _ := fx.deck([]string{"W"}, 3, "")
	fx.add(b3, held1, "main", 1)
	fx.add(b3, held2, "main", 1)
	fx.assistant(capture(&seen))
	fx.propose(b3, map[string]any{"step": "picks"})
	pool = poolNames(seen.Pool)
	for _, g := range []wizCard{g1, g2, g3} {
		wantIn(t, pool, "bracket 3, one left", g)
	}
	if seen.GameChangersLeft == nil || *seen.GameChangersLeft != 1 {
		t.Errorf("Game Changers left sent to the model = %v, want 1", seen.GameChangersLeft)
	}
	fx.assistant(pickNames(g1.Name, g2.Name, g3.Name, control.Name))
	got := fx.propose(b3, map[string]any{"step": "picks"})
	names := proposalNames(got.Proposals)
	if len(names) != 2 || names[0] != g1.Name || names[1] != control.Name {
		t.Errorf("proposals = %v, want [%s %s] (second and third Game Changer dropped)", names, g1.Name, control.Name)
	}
	if got.Dropped != 2 || !got.Proposals[0].GameChanger || got.Proposals[1].GameChanger {
		t.Errorf("dropped = %d, flags = %v/%v", got.Dropped, got.Proposals[0].GameChanger, got.Proposals[1].GameChanger)
	}
	if got.GameChangers.Limit == nil || *got.GameChangers.Limit != 3 || got.GameChangers.InDeck != 2 || got.GameChangers.Left == nil || *got.GameChangers.Left != 1 {
		t.Errorf("game_changers = %+v", got.GameChangers)
	}

	// Bracket 4: unlimited, and the Game Changers are offered.
	b4, _ := fx.deck([]string{"W"}, 4, "")
	fx.assistant(pickNames(g1.Name, g2.Name, g3.Name))
	got = fx.propose(b4, map[string]any{"step": "picks"})
	if len(got.Proposals) != 3 || got.GameChangers.Limit != nil || got.GameChangers.Left != nil {
		t.Errorf("bracket 4: %d proposals, game_changers = %+v", len(got.Proposals), got.GameChangers)
	}
}

// Admission limits what the wizard proposes, not what the deck accepts: the add
// endpoint still takes a Game Changer into a Bracket 2 deck, and the bracket
// report calls it out.
//
// @spec WIZ-025
func TestWizardAdmission_NeverBlocksADeckMutation(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "")
	g := fx.card(wizCardSpec{name: "Manual GC", typeLine: "Artifact", identity: []string{"W"}, gc: true})

	rec := serve(t, fx.a, fx.owner, http.MethodPost, "/decks/"+deckID+"/cards", map[string]any{"card_id": uuidString(g.ID), "board": "main"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("adding a Game Changer to a Bracket 2 deck = %d %s, want 201", rec.Code, rec.Body.String())
	}
	if p := fx.get(deckID); p.Policy.GameChangersInDeck != 1 || p.Policy.GameChangersLeft == nil || *p.Policy.GameChangersLeft != 0 {
		t.Errorf("policy after the manual add = %+v", p.Policy)
	}
}

// @spec WIZ-021, WIZ-022, WIZ-023, WIZ-024
func TestWizardPropose_ComboAdmission(t *testing.T) {
	fx := newWizFixture(t)
	held := fx.card(wizCardSpec{name: "Combo Held", identity: []string{"W"}})
	finisher := fx.card(wizCardSpec{name: "Combo Finisher", identity: []string{"W"}})
	partnerA := fx.card(wizCardSpec{name: "Combo Pair A", identity: []string{"W"}})
	partnerB := fx.card(wizCardSpec{name: "Combo Pair B", identity: []string{"W"}})
	control := fx.card(wizCardSpec{name: "Combo Control", identity: []string{"W"}})
	fx.combo(3, held, finisher)     // fast two-card combo: Bracket 4+
	fx.combo(3, partnerA, partnerB) // fine alone; together a Bracket 4+ combo

	var seen ai.CurateRequest
	b3, _ := fx.deck([]string{"W"}, 3, "")
	fx.add(b3, held, "main", 1)
	fx.assistant(capture(&seen))
	resp := fx.propose(b3, map[string]any{"step": "picks"})
	pool := poolNames(seen.Pool)
	wantIn(t, pool, "bracket 3", control)
	wantOut(t, pool, "bracket 3 (completes a fast combo)", finisher)
	wantIn(t, pool, "bracket 3 (half of a pair)", partnerA)
	wantIn(t, pool, "bracket 3 (half of a pair)", partnerB)
	if !resp.CombosChecked {
		t.Error("combos_checked = false with a combo in the mirror")
	}

	// The pair is admissible one card at a time but not together.
	fx.assistant(pickNames(partnerA.Name, partnerB.Name, control.Name))
	got := fx.propose(b3, map[string]any{"step": "picks"})
	names := proposalNames(got.Proposals)
	if len(names) != 2 || names[0] != partnerA.Name || names[1] != control.Name || got.Dropped != 1 {
		t.Errorf("proposals = %v dropped %d, want [A control] and 1 dropped", names, got.Dropped)
	}

	// Bracket 4 allows the combo.
	b4, _ := fx.deck([]string{"W"}, 4, "")
	fx.add(b4, held, "main", 1)
	fx.assistant(capture(&seen))
	fx.propose(b4, map[string]any{"step": "picks"})
	wantIn(t, poolNames(seen.Pool), "bracket 4", finisher)
}

// @spec WIZ-024
func TestWizardPropose_CombosCheckedFollowsTheComboMirror(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "")
	fx.card(wizCardSpec{name: "Mirror Probe", identity: []string{"W"}})
	fx.assistant(pickNames())

	var available bool
	if err := fx.a.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM combos)`).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if got := fx.propose(deckID, map[string]any{"step": "picks"}); got.CombosChecked != available {
		t.Errorf("combos_checked = %v, want %v (the mirror's state)", got.CombosChecked, available)
	}
}

// ---- propose: lands -----------------------------------------------------

// @spec WIZ-040, WIZ-042
func TestWizardPropose_FixingPool(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W", "U"}, 3, "")
	fetchTag := fx.tag("fetchland", nil)

	dual := fx.card(wizCardSpec{name: "Fix Dual", typeLine: "Land", identity: []string{"W", "U"}, produced: []string{"W", "U"}, usd: f64(20)})
	cheapDual := fx.card(wizCardSpec{name: "Fix Cheap Dual", typeLine: "Land", identity: []string{"W", "U"}, produced: []string{"W", "U"}, usd: f64(0.5)})
	mono := fx.card(wizCardSpec{name: "Fix Mono", typeLine: "Land", identity: []string{"W"}, produced: []string{"W"}})
	basic := fx.card(wizCardSpec{name: "Fix Basic", typeLine: "Basic Land — Island", identity: []string{"U"}, produced: []string{"U"}})
	tri := fx.card(wizCardSpec{name: "Fix Tri", typeLine: "Land", identity: []string{"W", "U", "B"}, produced: []string{"W", "U", "B"}})
	fetch := fx.card(wizCardSpec{name: "Fix Fetch", typeLine: "Land", identity: []string{"W", "U"}})
	fx.tagCard(fetchTag, fetch) // a fetch land the community tag knows
	textFetch := fx.card(wizCardSpec{
		name: "Fix Text Fetch", typeLine: "Land", identity: []string{"W", "U"},
		text: "{T}, Pay 1 life, Sacrifice Fix Text Fetch: Search your library for a Plains or Island card, put it onto the battlefield, then shuffle.",
	}) // one only its Oracle text identifies
	ghost := fx.card(wizCardSpec{
		name: "Fix Ghost", typeLine: "Land", identity: []string{},
		text: "{T}, Sacrifice Fix Ghost: Destroy target land. Its controller may search their library for a basic land card.",
	}) // sacrifices, but does not search its own library
	spell := fx.card(wizCardSpec{name: "Fix Spell", typeLine: "Instant", identity: []string{"W", "U"}, produced: []string{"W", "U"}})
	banned := fx.card(wizCardSpec{name: "Fix Banned", typeLine: "Land", identity: []string{"W", "U"}, produced: []string{"W", "U"}, legal: "banned"})
	onDeck := fx.card(wizCardSpec{name: "Fix On Deck", typeLine: "Land", identity: []string{"W", "U"}, produced: []string{"W", "U"}})
	fx.add(deckID, onDeck, "main", 1)

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))

	fx.propose(deckID, map[string]any{"step": "fixing", "include_fetch": false})
	pool := poolNames(seen.Pool)
	wantIn(t, pool, "fixing", dual)
	wantIn(t, pool, "fixing", cheapDual)
	for label, c := range map[string]wizCard{"mono": mono, "basic": basic, "off-identity tri": tri, "fetch (not allowed)": fetch, "text fetch (not allowed)": textFetch, "ghost": ghost, "spell": spell, "banned": banned, "on deck": onDeck} {
		wantOut(t, pool, "fixing/"+label, c)
	}
	if seen.Step != "fixing" || seen.StepGuidance == "" || seen.Want != 10 {
		t.Errorf("request = step %q want %d", seen.Step, seen.Want)
	}

	fx.propose(deckID, map[string]any{"step": "fixing", "include_fetch": true})
	pool = poolNames(seen.Pool)
	wantIn(t, pool, "fixing with fetch", fetch)
	wantIn(t, pool, "fixing with fetch (by Oracle text)", textFetch)
	wantOut(t, pool, "fixing with fetch (not a fetch)", ghost)

	fx.propose(deckID, map[string]any{"step": "fixing", "restrictions": map[string]any{"max_price_usd": 5}})
	pool = poolNames(seen.Pool)
	wantIn(t, pool, "fixing under a price cap", cheapDual)
	wantOut(t, pool, "fixing under a price cap", dual)

	// Room for the model's ask follows the land target (WIZ-042).
	basicLand := fx.card(wizCardSpec{name: "Fix Plains", typeLine: "Basic Land — Plains", identity: []string{"W"}, produced: []string{"W"}})
	fx.add(deckID, basicLand, "main", 35)
	fx.propose(deckID, map[string]any{"step": "fixing", "land_target": 37}) // 35 basics + the dual on deck = 36 lands
	if seen.Want != 1 {
		t.Errorf("want with one land slot left = %d, want 1", seen.Want)
	}
}

// @spec WIZ-041
func TestWizardPropose_UtilityPool(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W", "U"}, 3, "")
	utility := fx.tag("utility-land", nil)
	child := fx.tag("utility-land-sub", &utility)

	manland := fx.card(wizCardSpec{name: "Util Manland", typeLine: "Land", identity: []string{"W"}, produced: []string{"W"}})
	fx.tagCard(utility, manland)
	subtyped := fx.card(wizCardSpec{name: "Util Child", typeLine: "Land", identity: []string{}})
	fx.tagCard(child, subtyped)
	untagged := fx.card(wizCardSpec{name: "Util Untagged", typeLine: "Land", identity: []string{}})
	offIdentity := fx.card(wizCardSpec{name: "Util Off Identity", typeLine: "Land", identity: []string{"B"}})
	fx.tagCard(utility, offIdentity)
	taggedSpell := fx.card(wizCardSpec{name: "Util Spell", typeLine: "Creature — Test", identity: []string{}})
	fx.tagCard(utility, taggedSpell)

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))
	fx.propose(deckID, map[string]any{"step": "utility"})
	pool := poolNames(seen.Pool)
	wantIn(t, pool, "utility", manland)
	wantIn(t, pool, "utility (descendant tag)", subtyped)
	for label, c := range map[string]wizCard{"untagged": untagged, "off-identity": offIdentity, "nonland": taggedSpell} {
		wantOut(t, pool, "utility/"+label, c)
	}
	if seen.Want != 5 {
		t.Errorf("utility want = %d, want 5", seen.Want)
	}
}

// ---- propose: synergy ---------------------------------------------------

// @spec WIZ-043, WIZ-045, WIZ-033
func TestWizardPropose_PicksUseStoredSynergyWithAttribution(t *testing.T) {
	fx := newWizFixture(t)
	withSynergy(t, fx.a, true)
	deckID, cmd := fx.deck([]string{"W"}, 3, "")
	cleanupSynergy(t, fx.a, cmd.Oracle)

	strong := fx.card(wizCardSpec{name: "Syn Strong", identity: []string{"W"}})
	staple := fx.card(wizCardSpec{name: "Syn Staple", identity: []string{"W"}})
	synLand := fx.card(wizCardSpec{name: "Syn Land", typeLine: "Land", identity: []string{"W"}, produced: []string{"W"}})
	offColour := fx.card(wizCardSpec{name: "Syn Off Colour", identity: []string{"B"}})
	banned := fx.card(wizCardSpec{name: "Syn Banned", identity: []string{"W"}, legal: "banned"})
	popularOnly := fx.card(wizCardSpec{name: "Syn Popular Only", identity: []string{"W"}})

	ctx := context.Background()
	fetched := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Second), Valid: true}
	if err := fx.a.Queries.UpsertSynergyCommander(ctx, db.UpsertSynergyCommanderParams{
		CommanderOracleID: cmd.Oracle, CommanderName: cmd.Name, ColorIdentity: "W", DeckCount: 38, DuplicatesSkipped: 1, FetchedAt: fetched,
	}); err != nil {
		t.Fatal(err)
	}
	var rows []db.InsertSynergyCommanderCardsParams
	for _, r := range []struct {
		c    wizCard
		n    int32
		rate float64
	}{{strong, 36, 0.95}, {staple, 38, 1.0}, {synLand, 30, 0.8}, {offColour, 20, 0.5}, {banned, 20, 0.5}} {
		rows = append(rows, db.InsertSynergyCommanderCardsParams{CommanderOracleID: cmd.Oracle, CardOracleID: r.c.Oracle, DeckCount: r.n, InclusionRate: r.rate})
	}
	if _, err := fx.a.Queries.InsertSynergyCommanderCards(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.a.Pool.Exec(ctx, `
		UPDATE synergy_commander_cards
		SET baseline_rate = CASE card_oracle_id WHEN $2 THEN 0.08 WHEN $3 THEN 1.0 ELSE 0.1 END
		WHERE commander_oracle_id = $1`, cmd.Oracle, strong.Oracle, staple.Oracle); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.a.Pool.Exec(ctx, `UPDATE synergy_commander_cards SET synergy = inclusion_rate - baseline_rate WHERE commander_oracle_id = $1`, cmd.Oracle); err != nil {
		t.Fatal(err)
	}

	var seen ai.CurateRequest
	fx.assistant(func(r ai.CurateRequest) (ai.CurateResult, error) {
		seen = r
		return ai.CurateResult{Summary: "s", Picks: []ai.Suggestion{{Name: strong.Name, Reason: "r"}}, Usage: testUsage}, nil
	})
	got := fx.propose(deckID, map[string]any{"step": "picks"})

	if got.Source != "synergy" {
		t.Errorf("source = %q, want synergy", got.Source)
	}
	pool := poolNames(seen.Pool)
	wantIn(t, pool, "synergy pool", strong)
	wantIn(t, pool, "synergy pool", staple)
	for label, c := range map[string]wizCard{"land": synLand, "off-colour": offColour, "banned": banned, "popular-only": popularOnly} {
		wantOut(t, pool, "synergy pool/"+label, c)
	}
	// Ordered by synergy: the strong card leads, with its numbers in the prompt.
	if len(seen.Pool) < 2 || seen.Pool[0].Name != strong.Name || seen.Pool[0].InclusionPct == nil || *seen.Pool[0].InclusionPct != 95 ||
		seen.Pool[0].SynergyPct == nil || *seen.Pool[0].SynergyPct != 87 {
		t.Fatalf("pool head = %+v", seen.Pool)
	}

	if len(got.Proposals) != 1 || got.Proposals[0].Synergy == nil || got.Proposals[0].Synergy.Decks != 36 || got.Proposals[0].Synergy.Inclusion != 0.95 {
		t.Fatalf("proposal synergy = %+v", got.Proposals)
	}
	if got.Synergy == nil || got.Synergy.Status != "ready" || got.Synergy.Sample == nil || got.Synergy.Sample.DeckCount != 38 ||
		got.Synergy.Attribution["text"] != "Synergy data from public Archidekt decks" || got.Synergy.Attribution["url"] != "https://archidekt.com" {
		t.Errorf("synergy block = %+v", got.Synergy)
	}

	// With no stored sample the same deck falls back to popular picks, and says so.
	other, otherCmd := fx.deck([]string{"W"}, 3, "")
	cleanupSynergy(t, fx.a, otherCmd.Oracle)
	fx.assistant(capture(&seen))
	got = fx.propose(other, map[string]any{"step": "picks"})
	if got.Source != "popular" || got.Synergy != nil {
		t.Errorf("no sample: source = %q synergy = %+v, want popular and no synergy block", got.Source, got.Synergy)
	}
	wantIn(t, poolNames(seen.Pool), "popular pool", popularOnly)
	for _, c := range seen.Pool {
		if c.InclusionPct != nil || c.SynergyPct != nil {
			t.Errorf("popular pool card %q carries synergy numbers", c.Name)
		}
	}
}

// ---- basics -------------------------------------------------------------

// ensureBasic returns the mirror's basic land of that name, creating it when the
// mirror has none (CI loads no card data). It never deletes: other tests share it.
func ensureBasic(t *testing.T, a *API, name, typeLine string, color string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	if c, err := a.Queries.ResolveCardByName(ctx, name); err == nil {
		return c.ID
	}
	c, err := a.Queries.UpsertCard(ctx, db.UpsertCardParams{
		ScryfallOracleID: randUUID(t), Name: name, TypeLine: typeLine,
		Colors: []string{}, ColorIdentity: []string{color}, ProducedMana: []string{color}, Keywords: []string{},
		Legalities: json.RawMessage(`{"commander":"legal"}`), Layout: "normal",
	})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return c.ID
}

func (fx *wizFixture) basics(deckID string, body any) *httptest.ResponseRecorder {
	fx.t.Helper()
	return aiServe(fx.t, fx.a, fx.owner, "", http.MethodPost, "/decks/"+deckID+"/wizard/basics", body)
}

// @spec WIZ-050, WIZ-053
func TestWizardBasics_SplitsByPipsAndWritesNothing(t *testing.T) {
	fx := newWizFixture(t)
	plains := ensureBasic(t, fx.a, "Plains", "Basic Land — Plains", "W")
	island := ensureBasic(t, fx.a, "Island", "Basic Land — Island", "U")

	deckID, cmd := fx.deck([]string{"W", "U"}, 3, "midrange_value")
	if _, err := fx.a.Pool.Exec(context.Background(), `UPDATE cards SET mana_cost = '{2}{U}{U}' WHERE id = $1`, cmd.ID); err != nil {
		t.Fatal(err)
	}
	// Blue pips: 2 (commander) + 4 + 2; white pips: 2.
	for i, cost := range []string{"{1}{U}{U}{U}{U}", "{U}{U}", "{1}{W}{W}"} {
		c := fx.card(wizCardSpec{name: fmt.Sprintf("Pip Spell %d", i), identity: []string{"W", "U"}, manaCost: cost})
		fx.add(deckID, c, "main", 1)
	}
	fx.add(deckID, wizCard{ID: plains}, "main", 7) // 7 Plains already

	rec := fx.basics(deckID, map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("basics: %d %s", rec.Code, rec.Body.String())
	}
	got := decode[wizardBasicsResponse](t, rec)
	if got.LandTarget != 37 || got.LandsInDeck != 7 || got.BasicsNeeded != 30 {
		t.Fatalf("plan = %+v, want target 37, 7 lands, 30 needed", got)
	}
	byName := map[string]wizardBasicsRow{}
	total := 0
	for _, r := range got.Rows {
		byName[r.Card.Name] = r
		total += r.Quantity
	}
	if total != 30 {
		t.Errorf("rows sum to %d, want 30", total)
	}
	// 8 blue pips to 2 white: blue gets the larger share.
	if byName["Island"].Quantity <= byName["Plains"].Quantity || byName["Plains"].Quantity < 1 {
		t.Errorf("split = Plains %d / Island %d, want blue-heavy with at least one Plains", byName["Plains"].Quantity, byName["Island"].Quantity)
	}
	if byName["Plains"].InDeck != 7 || byName["Island"].InDeck != 0 {
		t.Errorf("in_deck = %d / %d, want 7 / 0", byName["Plains"].InDeck, byName["Island"].InDeck)
	}
	if byName["Island"].Card.ID != uuidString(island) {
		t.Errorf("Island row names %s", byName["Island"].Card.ID)
	}

	// An explicit land target changes the need; the deck itself is untouched.
	got = decode[wizardBasicsResponse](t, fx.basics(deckID, map[string]any{"land_target": 30}))
	if got.LandTarget != 30 || got.BasicsNeeded != 23 {
		t.Errorf("override plan = %+v", got)
	}
	var qty int
	if err := fx.a.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity), 0) FROM deck_cards WHERE deck_id = $1 AND card_id = $2`, deckID, island).Scan(&qty); err != nil || qty != 0 {
		t.Errorf("Island in deck after planning = %d (%v), want 0: the endpoint must not write", qty, err)
	}

	for _, bad := range []int{19, 61} {
		if rec := fx.basics(deckID, map[string]any{"land_target": bad}); rec.Code != http.StatusBadRequest {
			t.Errorf("land_target %d = %d, want 400", bad, rec.Code)
		}
	}
}

// @spec WIZ-004, WIZ-050
func TestWizardBasics_NeedsNoAINorABracketAndNothingNeededIsEmpty(t *testing.T) {
	fx := newWizFixture(t)
	ensureBasic(t, fx.a, "Plains", "Basic Land — Plains", "W")
	deckID, _ := fx.deck([]string{"W"}, 0, "") // no bracket: the basics plan does not need one
	fx.a.AI = ai.Disabled()

	if rec := fx.basics(deckID, nil); rec.Code != http.StatusOK {
		t.Errorf("empty body = %d %s, want 200", rec.Code, rec.Body.String())
	}

	plains, err := fx.a.Queries.ResolveCardByName(context.Background(), "Plains")
	if err != nil {
		t.Fatal(err)
	}
	fx.add(deckID, wizCard{ID: plains.ID}, "main", 40)
	got := decode[wizardBasicsResponse](t, fx.basics(deckID, map[string]any{}))
	if got.BasicsNeeded != 0 || len(got.Rows) != 0 {
		t.Errorf("deck over the target: %+v, want nothing needed", got)
	}
}

// @spec WIZ-054
func TestWizardBasics_NoCommanderIs422(t *testing.T) {
	fx := newWizFixture(t)
	if rec := fx.basics(fx.emptyDeck(), map[string]any{}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("basics with no commander = %d, want 422", rec.Code)
	}
}

// @spec WIZ-051, WIZ-052
func TestWizardBasics_ColourlessUsesWastesAndReportsMissing(t *testing.T) {
	fx := newWizFixture(t)
	ctx := context.Background()
	// Take any Wastes out of the way for the "missing" half of the test.
	var existed bool
	if err := fx.a.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cards WHERE name = 'Wastes')`).Scan(&existed); err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Skip("a Wastes card already exists in the shared mirror; the missing-card path cannot be exercised")
	}
	deckID, _ := fx.deck(nil, 3, "")

	got := decode[wizardBasicsResponse](t, fx.basics(deckID, map[string]any{}))
	if len(got.Rows) != 0 || len(got.Missing) != 1 || got.Missing[0] != "Wastes" || got.BasicsNeeded != 37 {
		t.Fatalf("no Wastes in the mirror: %+v, want a missing Wastes row", got)
	}

	wastes := fx.card(wizCardSpec{name: "Wastes Probe", typeLine: "Basic Land", produced: []string{"C"}})
	if _, err := fx.a.Pool.Exec(ctx, `UPDATE cards SET name = 'Wastes' WHERE id = $1`, wastes.ID); err != nil {
		t.Fatal(err)
	}
	got = decode[wizardBasicsResponse](t, fx.basics(deckID, map[string]any{}))
	if len(got.Rows) != 1 || got.Rows[0].Card.Name != "Wastes" || got.Rows[0].Quantity != 37 || len(got.Missing) != 0 {
		t.Errorf("with Wastes: %+v", got)
	}
}

// ---- persisted answers (WIZ-092, WIZ-100..104) --------------------------

func (fx *wizFixture) stateRaw(deckID, method string, body any) *httptest.ResponseRecorder {
	fx.t.Helper()
	return aiServe(fx.t, fx.a, fx.owner, "", method, "/decks/"+deckID+"/wizard/state", body)
}

func validWizardState() map[string]any {
	return map[string]any{
		"step":              "utility",
		"restrictions":      map[string]any{"no_tutors": true, "max_price_usd": 5.5, "notes": "goblin tokens"},
		"fixing":            "include",
		"include_fetch":     false,
		"utility":           true,
		"land_target":       36,
		"rejected_card_ids": []string{"0b1f6a2e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"},
	}
}

// @spec WIZ-092, WIZ-100, WIZ-101, WIZ-103
func TestWizardState_SaveRestoreReset(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W", "U"}, 3, "")
	calls := fx.assistant(shouldNotBeCalled(t))

	rec := fx.stateRaw(deckID, http.MethodGet, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get empty: %d %s", rec.Code, rec.Body.String())
	}
	if got := decode[wizardStateResponse](t, rec); got.Saved || got.State != nil {
		t.Errorf("fresh deck state = %+v, want saved false and no state", got)
	}

	if rec := fx.stateRaw(deckID, http.MethodPut, validWizardState()); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	got := decode[wizardStateResponse](t, fx.stateRaw(deckID, http.MethodGet, nil))
	if !got.Saved || got.State == nil || got.UpdatedAt == nil {
		t.Fatalf("after put: %+v, want saved with state and updated_at", got)
	}
	s := got.State
	if s.Step != "utility" || s.Fixing != "include" || s.IncludeFetch || !s.Utility ||
		s.LandTarget == nil || *s.LandTarget != 36 || !s.Restrictions.NoTutors ||
		s.Restrictions.MaxPriceUSD == nil || *s.Restrictions.MaxPriceUSD != 5.5 ||
		s.Restrictions.Notes != "goblin tokens" || len(s.RejectedCardIDs) != 1 {
		t.Errorf("restored state = %+v", s)
	}

	// A second save replaces the first, including clearing the land target.
	next := validWizardState()
	next["step"] = "picks"
	next["land_target"] = nil
	next["rejected_card_ids"] = []string{}
	if rec := fx.stateRaw(deckID, http.MethodPut, next); rec.Code != http.StatusOK {
		t.Fatalf("put 2: %d %s", rec.Code, rec.Body.String())
	}
	got = decode[wizardStateResponse](t, fx.stateRaw(deckID, http.MethodGet, nil))
	if got.State.Step != "picks" || got.State.LandTarget != nil || len(got.State.RejectedCardIDs) != 0 {
		t.Errorf("replaced state = %+v", got.State)
	}

	if rec := fx.stateRaw(deckID, http.MethodDelete, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if got := decode[wizardStateResponse](t, fx.stateRaw(deckID, http.MethodGet, nil)); got.Saved {
		t.Errorf("after delete: saved = true")
	}
	// Reset leaves the deck alone.
	if p := fx.get(deckID); p.Bracket == nil || *p.Bracket != 3 {
		t.Errorf("bracket after reset = %v, want 3", p.Bracket)
	}
	if rec := fx.stateRaw(deckID, http.MethodDelete, nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete when nothing saved = %d, want 204", rec.Code)
	}
	if *calls != 0 {
		t.Errorf("state endpoints made %d model calls, want 0", *calls)
	}
}

// @spec WIZ-100, WIZ-101, WIZ-103
func TestWizardState_OwnerOnly(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "")
	if rec := fx.stateRaw(deckID, http.MethodPut, validWizardState()); rec.Code != http.StatusOK {
		t.Fatalf("put: %d", rec.Code)
	}

	stranger := makeUser(t, fx.a)
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		var body any
		if m == http.MethodPut {
			body = validWizardState()
		}
		if rec := aiServe(t, fx.a, stranger, "", m, "/decks/"+deckID+"/wizard/state", body); rec.Code != http.StatusNotFound {
			t.Errorf("non-owner %s = %d, want 404", m, rec.Code)
		}
	}
	// The stranger's attempts changed nothing.
	if got := decode[wizardStateResponse](t, fx.stateRaw(deckID, http.MethodGet, nil)); !got.Saved {
		t.Errorf("owner's state was removed by a non-owner")
	}

	// An anonymous draft's owner can save and read its own state.
	token := "wiz-state-anon-" + fx.suffix
	rec := aiServe(t, fx.a, pgtype.UUID{}, token, http.MethodPost, "/decks", map[string]string{"name": "Anon Wizard Deck"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("anon create: %d %s", rec.Code, rec.Body.String())
	}
	anonDeck := decode[deckJSON](t, rec).ID
	fx.deckIDs = append(fx.deckIDs, anonDeck)
	path := "/decks/" + anonDeck + "/wizard/state"
	if rec := aiServe(t, fx.a, pgtype.UUID{}, token, http.MethodPut, path, validWizardState()); rec.Code != http.StatusOK {
		t.Errorf("anon put = %d, want 200", rec.Code)
	}
	if rec := aiServe(t, fx.a, pgtype.UUID{}, token, http.MethodGet, path, nil); rec.Code != http.StatusOK ||
		!decode[wizardStateResponse](t, rec).Saved {
		t.Errorf("anon get = %d %s", rec.Code, rec.Body.String())
	}
}

// @spec WIZ-102
func TestWizardState_Validation(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "")
	if rec := fx.stateRaw(deckID, http.MethodPut, validWizardState()); rec.Code != http.StatusOK {
		t.Fatalf("put: %d", rec.Code)
	}

	tooMany := make([]string, 201)
	for i := range tooMany {
		tooMany[i] = "0b1f6a2e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	}
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	cases := map[string]func(m map[string]any){
		"unknown step":     func(m map[string]any) { m["step"] = "roles" },
		"empty step":       func(m map[string]any) { delete(m, "step") },
		"unknown fixing":   func(m map[string]any) { m["fixing"] = "some" },
		"bad rejected id":  func(m map[string]any) { m["rejected_card_ids"] = []string{"nope"} },
		"too many ids":     func(m map[string]any) { m["rejected_card_ids"] = tooMany },
		"long notes":       func(m map[string]any) { m["restrictions"] = map[string]any{"notes": string(long)} },
		"zero price":       func(m map[string]any) { m["restrictions"] = map[string]any{"max_price_usd": 0} },
		"negative price":   func(m map[string]any) { m["restrictions"] = map[string]any{"max_price_usd": -1} },
		"low land target":  func(m map[string]any) { m["land_target"] = 19 },
		"high land target": func(m map[string]any) { m["land_target"] = 61 },
	}
	for name, mutate := range cases {
		body := validWizardState()
		mutate(body)
		if rec := fx.stateRaw(deckID, http.MethodPut, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: put = %d, want 400", name, rec.Code)
		}
	}
	// None of the refused saves touched the stored state.
	got := decode[wizardStateResponse](t, fx.stateRaw(deckID, http.MethodGet, nil))
	if got.State == nil || got.State.Step != "utility" {
		t.Errorf("state after refused saves = %+v, want the original", got.State)
	}
}

// @spec WIZ-104
func TestWizardState_GoesWithTheDeck(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "")
	if rec := fx.stateRaw(deckID, http.MethodPut, validWizardState()); rec.Code != http.StatusOK {
		t.Fatalf("put: %d", rec.Code)
	}
	if _, err := fx.a.Pool.Exec(context.Background(), `DELETE FROM decks WHERE id = $1`, deckID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := fx.a.Pool.QueryRow(context.Background(), `SELECT count(*) FROM deck_wizard_state WHERE deck_id = $1`, deckID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("deck_wizard_state rows after deck delete = %d, want 0", n)
	}
}
