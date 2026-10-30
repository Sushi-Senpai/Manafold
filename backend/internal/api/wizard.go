package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/ai"
	"manafold-backend/internal/bracket"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckstats"
	"manafold-backend/internal/deckwizard"
	"manafold-backend/internal/synergy"
)

// Wizard limits (WIZ-012, WIZ-044).
const (
	defaultWizardDailyLimit = 40
	maxWizardNotesRunes     = 500
	maxWizardRejected       = 200
	// wizardRawFactor is how many candidates a pool query fetches per slot the
	// pool will finally show, so the filters (price, tutors, bracket admission)
	// can discard most of them and still leave a full pool.
	wizardRawFactor = 4
	// wizardGameChangerSlots is how many identity-legal Game Changers the picks
	// pool adds beyond the synergy / popularity ranking (WIZ-044).
	wizardGameChangerSlots = 10
	// wizardDeckSize is the Commander deck size; a deck at or past it is full.
	wizardDeckSize = 100
)

// RegisterWizardRoutes mounts the deck wizard endpoints. It is called inside the
// authenticated /api group. The wizard proposes cards and never writes a deck:
// the player's Accept goes through the ordinary add-card endpoint.
//
// @spec WIZ-002, WIZ-010, WIZ-050, WIZ-100, PLATFORM-005
func (a *API) RegisterWizardRoutes(r chi.Router) {
	r.Get("/decks/{id}/wizard", a.getWizardPlan)
	r.Post("/decks/{id}/wizard/propose", a.wizardPropose)
	r.Post("/decks/{id}/wizard/basics", a.wizardBasics)
	r.Get("/decks/{id}/wizard/state", a.getWizardState)
	r.Put("/decks/{id}/wizard/state", a.putWizardState)
	r.Delete("/decks/{id}/wizard/state", a.deleteWizardState)
}

// ---- JSON shapes --------------------------------------------------------

type wizardPolicyJSON struct {
	GameChangerLimit   *int   `json:"game_changer_limit"`
	GameChangersInDeck int    `json:"game_changers_in_deck"`
	GameChangersLeft   *int   `json:"game_changers_left"`
	Guidance           string `json:"guidance"`
}

type wizardLandsJSON struct {
	Target           int  `json:"target"`
	BaseTarget       int  `json:"base_target"`
	InDeck           int  `json:"in_deck"`
	BasicsNeeded     int  `json:"basics_needed"`
	FixingApplicable bool `json:"fixing_applicable"`
}

// wizardFastManaJSON is the Bracket 4 fast-mana block of the plan (WIZ-113).
type wizardFastManaJSON struct {
	Applicable bool `json:"applicable"`
	InDeck     int  `json:"in_deck"`
}

type wizardSynergyStatusJSON struct {
	Status string  `json:"status"`
	Reason *string `json:"reason"`
}

type wizardPlanResponse struct {
	Ready     bool                     `json:"ready"`
	Reason    *string                  `json:"reason"`
	AIEnabled bool                     `json:"ai_enabled"`
	Bracket   *int                     `json:"bracket"`
	Archetype *string                  `json:"archetype"`
	Policy    *wizardPolicyJSON        `json:"policy"`
	Lands     wizardLandsJSON          `json:"lands"`
	FastMana  wizardFastManaJSON       `json:"fast_mana"`
	Synergy   *wizardSynergyStatusJSON `json:"synergy"`
}

type wizardRestrictions struct {
	NoTutors    bool     `json:"no_tutors"`
	MaxPriceUSD *float64 `json:"max_price_usd"`
	Notes       string   `json:"notes"`
}

type wizardProposeRequest struct {
	Step            string             `json:"step"`
	IncludeFetch    bool               `json:"include_fetch"`
	LandTarget      *int               `json:"land_target"`
	Restrictions    wizardRestrictions `json:"restrictions"`
	RejectedCardIDs []string           `json:"rejected_card_ids"`
}

type wizardProposalSynergy struct {
	Decks     int32    `json:"decks"`
	Inclusion float64  `json:"inclusion"`
	Synergy   *float64 `json:"synergy"`
}

type wizardProposal struct {
	Card        cardSummary            `json:"card"`
	Reason      string                 `json:"reason"`
	GameChanger bool                   `json:"game_changer"`
	PriceUSD    *float64               `json:"price_usd"`
	Synergy     *wizardProposalSynergy `json:"synergy"`
}

type wizardGameChangersJSON struct {
	Limit  *int `json:"limit"`
	InDeck int  `json:"in_deck"`
	Left   *int `json:"left"`
}

type wizardProposeSynergy struct {
	Status      string            `json:"status"`
	Sample      *synergySample    `json:"sample"`
	Attribution map[string]string `json:"attribution"`
}

type wizardProposeResponse struct {
	Step          string                 `json:"step"`
	Summary       string                 `json:"summary"`
	Source        string                 `json:"source"`
	Proposals     []wizardProposal       `json:"proposals"`
	Dropped       int                    `json:"dropped"`
	PoolSize      int                    `json:"pool_size"`
	EmptyReason   *string                `json:"empty_reason"`
	GameChangers  wizardGameChangersJSON `json:"game_changers"`
	CombosChecked bool                   `json:"combos_checked"`
	Synergy       *wizardProposeSynergy  `json:"synergy"`
	Model         string                 `json:"model"`
}

type wizardBasicsRequest struct {
	LandTarget *int `json:"land_target"`
}

type wizardBasicsRow struct {
	Card     cardSummary `json:"card"`
	Quantity int         `json:"quantity"`
	InDeck   int         `json:"in_deck"`
}

type wizardBasicsResponse struct {
	LandTarget   int               `json:"land_target"`
	LandsInDeck  int               `json:"lands_in_deck"`
	BasicsNeeded int               `json:"basics_needed"`
	Rows         []wizardBasicsRow `json:"rows"`
	Missing      []string          `json:"missing"`
}

// ---- the deck as the wizard reads it ------------------------------------

// wizardView is the deck's wizard-relevant numbers, derived once per request
// from the loaded deck.
type wizardView struct {
	ld       *loadedDeck
	identity []string
	bracket  int // 0 = none chosen
	policy   deckwizard.Policy
	policyOK bool
	stats    deckstats.Stats
	gcInDeck int
	// fastMana is the quantity of fast-mana cards on main and command. It is
	// loaded only at Bracket 4 and is zero elsewhere (WIZ-113).
	fastMana int
	// mainCount is the cards on main and command, commanders included.
	mainCount int
	// onDeck holds the id of every card on any board, the commanders included.
	onDeck map[string]bool
}

func newWizardView(ld *loadedDeck) wizardView {
	v := wizardView{
		ld:       ld,
		identity: nonNil(ld.deck.ColorIdentity),
		stats:    deckstats.Analyze(statsInputFor(ld)),
		onDeck:   map[string]bool{},
	}
	if ld.deck.Bracket.Valid {
		v.bracket = int(ld.deck.Bracket.Int32)
		v.policy, v.policyOK = deckwizard.PolicyFor(v.bracket)
	}
	v.mainCount = v.stats.LandCount + v.stats.NonLandCount
	for _, c := range []*db.Card{ld.commander, ld.partner} {
		if c != nil {
			v.onDeck[uuidString(c.ID)] = true
			if c.IsGameChanger {
				v.gcInDeck++
			}
		}
	}
	for _, e := range ld.entries {
		v.onDeck[uuidString(e.CardID)] = true
		if (e.Board == "main" || e.Board == "command") && e.IsGameChanger {
			v.gcInDeck += int(e.Quantity)
		}
	}
	return v
}

// notReadyReason is why the wizard cannot run on this deck, or "" when it can
// (WIZ-002, WIZ-011).
func (v wizardView) notReadyReason() string {
	switch {
	case v.ld.commander == nil:
		return "no_commander"
	case v.bracket == 0:
		return "no_bracket"
	case !v.policyOK:
		return "bracket_unsupported"
	}
	return ""
}

func (v wizardView) archetype() string { return v.ld.deck.Archetype.String }

func (v wizardView) baseLandTarget() int { return deckwizard.LandTarget(v.archetype()) }

// landTarget is the player's override, else the base target relaxed by the fast
// mana the deck holds at Bracket 4 (WIZ-114).
func (v wizardView) landTarget(override *int) int {
	if override != nil {
		return *override
	}
	return deckwizard.RelaxedLandTarget(v.baseLandTarget(), v.bracket, v.fastMana)
}

// loadFastMana counts the deck's fast-mana cards, at Bracket 4 only: below it the
// wizard never looks (WIZ-109, WIZ-113).
func (a *API) loadFastMana(ctx context.Context, v *wizardView) error {
	v.fastMana = 0
	if !deckwizard.StepFastMana.AppliesAtBracket(v.bracket) {
		return nil
	}
	qty := map[string]int{}
	var ids []pgtype.UUID
	for _, e := range v.ld.entries {
		if e.Board != "main" && e.Board != "command" {
			continue
		}
		key := uuidString(e.CardID)
		if qty[key] == 0 {
			ids = append(ids, e.CardID)
		}
		qty[key] += int(e.Quantity)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := a.Queries.FilterFastManaCardIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range rows {
		v.fastMana += qty[uuidString(id)]
	}
	return nil
}

func (v wizardView) gameChangers() wizardGameChangersJSON {
	out := wizardGameChangersJSON{InDeck: v.gcInDeck}
	if v.policyOK {
		out.Limit = v.policy.GameChangerLimit
		out.Left = deckwizard.GameChangersLeft(v.bracket, v.gcInDeck)
	}
	return out
}

// ---- plan ---------------------------------------------------------------

// getWizardPlan reports whether the wizard can run on a deck and the numbers it
// works from. It needs neither a model nor a sign-in (WIZ-004).
//
// @spec WIZ-002, WIZ-003, WIZ-004, WIZ-005, WIZ-113, WIZ-114
func (a *API) getWizardPlan(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	v := newWizardView(ld)
	if err := a.loadFastMana(r.Context(), &v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count the deck's fast mana")
		return
	}

	plan := wizardPlanResponse{
		AIEnabled: a.AI != nil && a.AI.Enabled(),
		Lands: wizardLandsJSON{
			Target:           v.landTarget(nil),
			BaseTarget:       v.baseLandTarget(),
			InDeck:           v.stats.LandCount,
			FixingApplicable: len(v.identity) >= 2,
		},
		FastMana: wizardFastManaJSON{
			Applicable: deckwizard.StepFastMana.AppliesAtBracket(v.bracket),
			InDeck:     v.fastMana,
		},
	}
	plan.Lands.BasicsNeeded = max(0, plan.Lands.Target-plan.Lands.InDeck)
	if v.bracket != 0 {
		b := v.bracket
		plan.Bracket = &b
	}
	if s := v.archetype(); s != "" {
		plan.Archetype = &s
	}
	if v.policyOK {
		gc := v.gameChangers()
		plan.Policy = &wizardPolicyJSON{
			GameChangerLimit:   gc.Limit,
			GameChangersInDeck: gc.InDeck,
			GameChangersLeft:   gc.Left,
			Guidance:           v.policy.Guidance,
		}
	}
	if reason := v.notReadyReason(); reason != "" {
		plan.Reason = &reason
	} else {
		plan.Ready = true
		// Start gathering synergy now so the picks step can use it (WIZ-005).
		if a.Synergy != nil {
			if sv, err := a.Synergy.Ensure(r.Context(), ld.commander.ID); err != nil {
				log.Printf("wizard: ensuring synergy for deck %s: %v", uuidString(deck.ID), err)
			} else {
				plan.Synergy = &wizardSynergyStatusJSON{Status: string(sv.State), Reason: reasonPtr(sv.Reason)}
			}
		}
	}
	writeJSON(w, http.StatusOK, plan)
}

// ---- propose ------------------------------------------------------------

// wizardCandidate is one card in a step's pool with what the pool knows of it.
type wizardCandidate struct {
	card  db.Card
	price *float64
	// syn is set only when the card came from stored synergy data.
	syn *wizardProposalSynergy
}

// wizardPool is a step's candidate menu plus the context its admission checks
// used.
type wizardPool struct {
	entries   []wizardCandidate
	source    string
	synergy   *synergy.CommanderView // set when source == "synergy"
	admission *wizardAdmission
}

// wizardPropose runs one curated round of one step: it builds the step's pool
// deterministically, asks the model to pick from it, and returns only the picks
// that pass the validate-every-returned-card gate, belong to the pool, and keep
// the deck inside its bracket. It writes nothing to the deck.
//
// @spec WIZ-010, WIZ-011, WIZ-012, WIZ-013, WIZ-014, WIZ-015, WIZ-020, WIZ-023, WIZ-025, WIZ-032, WIZ-042, WIZ-045, WIZ-109, WIZ-111, WIZ-112
func (a *API) wizardPropose(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.aiGateCaller(w, r)
	if !ok {
		return
	}
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	limit := a.AIWizardDailyLimit
	if limit <= 0 {
		limit = defaultWizardDailyLimit
	}
	if !a.aiGateQuota(w, r, "wizard", limit, uid) {
		return
	}

	req, step, rejected, msg := parseProposeRequest(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	v := newWizardView(ld)
	if reason := v.notReadyReason(); reason != "" {
		writeError(w, http.StatusUnprocessableEntity, wizardReasonText(reason))
		return
	}
	if !step.AppliesAtBracket(v.bracket) {
		writeError(w, http.StatusUnprocessableEntity, "fast mana is offered at Bracket 4 only")
		return
	}

	ctx := r.Context()
	if err := a.loadFastMana(ctx, &v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count the deck's fast mana")
		return
	}
	target := v.landTarget(req.LandTarget)
	want := step.Want()
	empty := func(reason string) {
		writeJSON(w, http.StatusOK, wizardProposeResponse{
			Step: string(step), Source: sourceFor(step, ""), Proposals: []wizardProposal{},
			EmptyReason: &reason, GameChangers: v.gameChangers(),
		})
	}
	switch {
	case step == deckwizard.StepFixing && len(v.identity) < 2:
		empty("no_fixing_needed")
		return
	case step.IsLandStep():
		room := target - v.stats.LandCount
		if room <= 0 {
			empty("land_target_met")
			return
		}
		want = min(want, room)
	case v.mainCount >= wizardDeckSize:
		empty("deck_full")
		return
	}

	pool, err := a.buildWizardPool(r, v, step, req, rejected)
	if err != nil {
		log.Printf("wizard: building %s pool for deck %s: %v", step, uuidString(deck.ID), err)
		writeError(w, http.StatusInternalServerError, "failed to build the candidate pool")
		return
	}
	gc := v.gameChangers()
	if len(pool.entries) == 0 {
		reason := "no_candidates"
		writeJSON(w, http.StatusOK, wizardProposeResponse{
			Step: string(step), Source: pool.source, Proposals: []wizardProposal{}, EmptyReason: &reason,
			GameChangers: gc, CombosChecked: pool.admission.combosChecked,
		})
		return
	}

	cr := ai.CurateRequest{
		Step:             string(step),
		StepGuidance:     step.Guidance(),
		CommanderName:    ld.commander.Name,
		CommanderText:    ld.commander.OracleText,
		DeckIdentity:     v.identity,
		Bracket:          v.bracket,
		BracketGuidance:  v.policy.Guidance,
		GameChangersLeft: gc.Left,
		Notes:            req.Restrictions.Notes,
		InDeck:           deckCardNames(ld),
		Pool:             poolCards(pool.entries),
		Want:             want,
	}
	res, err := a.AI.Curate(ctx, cr)
	a.meterIfBilled(r, uid, "wizard", res.Usage)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "AI features are not configured")
			return
		}
		writeError(w, http.StatusBadGateway, "the wizard could not get picks from the model")
		return
	}

	proposals, dropped, err := a.vetPicks(ctx, ld, v, pool, res.Picks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate the picks")
		return
	}
	if len(proposals) > want {
		proposals = proposals[:want]
	}

	out := wizardProposeResponse{
		Step:          string(step),
		Summary:       res.Summary,
		Source:        pool.source,
		Proposals:     proposals,
		Dropped:       dropped,
		PoolSize:      len(pool.entries),
		GameChangers:  gc,
		CombosChecked: pool.admission.combosChecked,
		Model:         res.Usage.Model,
	}
	if pool.synergy != nil {
		out.Synergy = &wizardProposeSynergy{
			Status:      string(pool.synergy.State),
			Sample:      sampleFrom(pool.synergy.Sample),
			Attribution: map[string]string{"text": synergy.AttributionText, "url": synergy.AttributionURL},
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// vetPicks is what stands between the model's answer and the player: the shared
// validate-every-returned-card gate (WIZ-020), pool membership (WIZ-020), and
// cumulative bracket admission in the model's order (WIZ-023). It returns the
// survivors and how many picks were dropped.
func (a *API) vetPicks(ctx context.Context, ld *loadedDeck, v wizardView, pool wizardPool, picks []ai.Suggestion) ([]wizardProposal, int, error) {
	gated, dropped, err := a.gateNames(ctx, picks, ld)
	if err != nil {
		return nil, 0, err
	}
	members := make(map[string]wizardCandidate, len(pool.entries))
	for _, e := range pool.entries {
		members[uuidString(e.card.ID)] = e
	}
	var inPool []gatedCard
	var cands []bracket.Card
	for _, g := range gated {
		if _, ok := members[uuidString(g.card.ID)]; !ok {
			dropped++
			continue
		}
		inPool = append(inPool, g)
		cands = append(cands, pool.admission.card(g.card))
	}
	admitted := deckwizard.AdmitInOrder(pool.admission.deck, v.bracket, cands)

	proposals := []wizardProposal{}
	for i, g := range inPool {
		if !admitted[i] {
			dropped++
			continue
		}
		m := members[uuidString(g.card.ID)]
		proposals = append(proposals, wizardProposal{
			Card:        cardSummaryFrom(g.card, g.print, g.havePrint),
			Reason:      g.reason,
			GameChanger: g.card.IsGameChanger,
			PriceUSD:    m.price,
			Synergy:     m.syn,
		})
	}
	return proposals, dropped, nil
}

func sourceFor(step deckwizard.Step, picksSource string) string {
	if step.IsLandStep() {
		return "lands"
	}
	if step == deckwizard.StepFastMana {
		return "fast_mana"
	}
	if picksSource == "" {
		return "popular"
	}
	return picksSource
}

func wizardReasonText(reason string) string {
	switch reason {
	case "no_commander":
		return "assign a commander before using the wizard"
	case "no_bracket":
		return "choose a bracket before using the wizard"
	}
	return "the wizard supports Brackets 1 through 4"
}

// validateWizardAnswers holds the checks a propose request and a saved wizard
// state share (WIZ-012, WIZ-102): the land target, the restrictions and the
// rejected set. A non-empty message is a 400.
func validateWizardAnswers(landTarget *int, restr wizardRestrictions, rejected []string) string {
	if landTarget != nil && !deckwizard.ValidLandTarget(*landTarget) {
		return "land_target must be between 20 and 60"
	}
	if p := restr.MaxPriceUSD; p != nil && (!(*p > 0) || math.IsInf(*p, 0)) {
		return "max_price_usd must be a positive number"
	}
	if utf8.RuneCountInString(restr.Notes) > maxWizardNotesRunes {
		return "notes must be at most 500 characters"
	}
	if len(rejected) > maxWizardRejected {
		return "at most 200 rejected_card_ids"
	}
	for _, s := range rejected {
		if _, ok := parseUUID(s); !ok {
			return "rejected_card_ids must be card ids"
		}
	}
	return ""
}

// parseProposeRequest decodes and validates a propose body. A non-empty message
// is a 400 (WIZ-012).
func parseProposeRequest(r *http.Request) (wizardProposeRequest, deckwizard.Step, []pgtype.UUID, string) {
	var req wizardProposeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		return req, "", nil, "request body must be a JSON object"
	}
	step, ok := deckwizard.ParseStep(req.Step)
	if !ok {
		return req, "", nil, "step must be one of fixing, utility, fastmana, picks"
	}
	if msg := validateWizardAnswers(req.LandTarget, req.Restrictions, req.RejectedCardIDs); msg != "" {
		return req, "", nil, msg
	}
	req.Restrictions.Notes = strings.Join(strings.Fields(req.Restrictions.Notes), " ")
	rejected := make([]pgtype.UUID, 0, len(req.RejectedCardIDs))
	for _, s := range req.RejectedCardIDs {
		id, _ := parseUUID(s)
		rejected = append(rejected, id)
	}
	return req, step, rejected, ""
}

// deckCardNames lists the names on every board for the model's "already in the
// deck" line.
func deckCardNames(ld *loadedDeck) []string {
	var names []string
	for _, c := range []*db.Card{ld.commander, ld.partner} {
		if c != nil {
			names = append(names, c.Name)
		}
	}
	for _, e := range ld.entries {
		names = append(names, e.Name)
	}
	return names
}

// deckCardIDs lists the id of every card on any board, the commanders included.
func deckCardIDs(ld *loadedDeck) []pgtype.UUID {
	var ids []pgtype.UUID
	for _, c := range []*db.Card{ld.commander, ld.partner} {
		if c != nil {
			ids = append(ids, c.ID)
		}
	}
	for _, e := range ld.entries {
		ids = append(ids, e.CardID)
	}
	return ids
}

func poolCards(entries []wizardCandidate) []ai.PoolCard {
	out := make([]ai.PoolCard, 0, len(entries))
	for _, e := range entries {
		pc := ai.PoolCard{
			CardRef:      cardRefFromCard(e.card),
			ProducedMana: nonNil(e.card.ProducedMana),
			GameChanger:  e.card.IsGameChanger,
			PriceUSD:     e.price,
		}
		if e.syn != nil {
			incl := int(math.Round(e.syn.Inclusion * 100))
			pc.InclusionPct = &incl
			if e.syn.Synergy != nil {
				pts := int(math.Round(*e.syn.Synergy * 100))
				pc.SynergyPct = &pts
			}
		}
		out = append(out, pc)
	}
	return out
}

// ---- pools --------------------------------------------------------------

// buildWizardPool assembles a step's candidate menu from the mirror and applies
// every deterministic rule to it: the deck's own cards and the player's
// rejections, the price cap, the tutor ban, and bracket admission.
//
// @spec WIZ-022, WIZ-030, WIZ-031, WIZ-033, WIZ-034, WIZ-040, WIZ-041, WIZ-043, WIZ-044, WIZ-110
func (a *API) buildWizardPool(r *http.Request, v wizardView, step deckwizard.Step, req wizardProposeRequest, rejected []pgtype.UUID) (wizardPool, error) {
	ctx := r.Context()
	excludeSet := make(map[string]bool, len(v.onDeck)+len(rejected))
	exclude := make([]pgtype.UUID, 0, len(v.onDeck)+len(rejected))
	for _, id := range deckCardIDs(v.ld) {
		if !excludeSet[uuidString(id)] {
			excludeSet[uuidString(id)] = true
			exclude = append(exclude, id)
		}
	}
	for _, id := range rejected {
		if !excludeSet[uuidString(id)] {
			excludeSet[uuidString(id)] = true
			exclude = append(exclude, id)
		}
	}

	size := step.PoolSize()
	raw := int32(size * wizardRawFactor)
	pool := wizardPool{source: sourceFor(step, "popular")}
	var main, extras []wizardCandidate

	switch step {
	case deckwizard.StepFixing:
		rows, err := a.Queries.ListWizardFixingLands(ctx, db.ListWizardFixingLandsParams{
			Identity: v.identity, ExcludeIds: exclude, IncludeFetch: req.IncludeFetch, Lim: raw,
		})
		if err != nil {
			return pool, err
		}
		main = candidatesOf(rows)
	case deckwizard.StepUtility:
		rows, err := a.Queries.ListWizardUtilityLands(ctx, db.ListWizardUtilityLandsParams{
			Identity: v.identity, ExcludeIds: exclude, Lim: raw,
		})
		if err != nil {
			return pool, err
		}
		main = candidatesOf(rows)
	case deckwizard.StepFastMana:
		rows, err := a.Queries.ListWizardFastMana(ctx, db.ListWizardFastManaParams{
			Identity: v.identity, ExcludeIds: exclude, Lim: raw,
		})
		if err != nil {
			return pool, err
		}
		main = candidatesOf(rows)
	default:
		var err error
		main, pool.synergy, err = a.picksFromSynergy(ctx, v, excludeSet)
		if err != nil {
			return pool, err
		}
		if pool.synergy != nil {
			pool.source = "synergy"
		} else {
			rows, err := a.Queries.ListWizardPopularNonlands(ctx, db.ListWizardPopularNonlandsParams{
				Identity: v.identity, ExcludeIds: exclude, Lim: raw,
			})
			if err != nil {
				return pool, err
			}
			main = candidatesOf(rows)
		}
		if left := deckwizard.GameChangersLeft(v.bracket, v.gcInDeck); left == nil || *left > 0 {
			rows, err := a.Queries.ListWizardGameChangers(ctx, db.ListWizardGameChangersParams{
				Identity: v.identity, ExcludeIds: exclude, Lim: int32(wizardGameChangerSlots * wizardRawFactor),
			})
			if err != nil {
				return pool, err
			}
			inMain := make(map[string]bool, len(main))
			for _, c := range main {
				inMain[uuidString(c.card.ID)] = true
			}
			for _, c := range candidatesOf(rows) {
				if !inMain[uuidString(c.card.ID)] {
					extras = append(extras, c)
				}
			}
		}
	}

	if err := a.attachPrices(ctx, main, extras); err != nil {
		return pool, err
	}
	keep := func(list []wizardCandidate, pred func(wizardCandidate) bool) []wizardCandidate {
		out := list[:0:0]
		for _, c := range list {
			if pred(c) {
				out = append(out, c)
			}
		}
		return out
	}
	if ceiling := req.Restrictions.MaxPriceUSD; ceiling != nil {
		underCap := func(c wizardCandidate) bool { return c.price != nil && *c.price <= *ceiling }
		main, extras = keep(main, underCap), keep(extras, underCap)
	}
	if req.Restrictions.NoTutors && len(main)+len(extras) > 0 {
		tutors, err := a.tutorIDs(ctx, append(append([]wizardCandidate(nil), main...), extras...))
		if err != nil {
			return pool, err
		}
		notTutor := func(c wizardCandidate) bool { return !tutors[uuidString(c.card.ID)] }
		main, extras = keep(main, notTutor), keep(extras, notTutor)
	}

	// Bracket admission: each candidate alone against the deck (WIZ-022).
	remaining := append(append([]wizardCandidate(nil), main...), extras...)
	cards := make([]db.Card, len(remaining))
	for i, c := range remaining {
		cards[i] = c.card
	}
	adm, err := a.newWizardAdmission(r, v, cards)
	if err != nil {
		return pool, err
	}
	pool.admission = adm
	bcards := make([]bracket.Card, len(cards))
	for i, c := range cards {
		bcards[i] = adm.card(c)
	}
	admitted := map[string]bool{}
	for i, ok := range deckwizard.Admit(adm.deck, v.bracket, bcards) {
		if ok {
			admitted[uuidString(cards[i].ID)] = true
		}
	}
	isAdmitted := func(c wizardCandidate) bool { return admitted[uuidString(c.card.ID)] }
	main, extras = keep(main, isAdmitted), keep(extras, isAdmitted)

	// Truncate: the Game Changer extras keep their slots (WIZ-044).
	nExtras := min(len(extras), wizardGameChangerSlots)
	if len(main) > size-nExtras {
		main = main[:size-nExtras]
	}
	pool.entries = append(append([]wizardCandidate(nil), main...), extras[:nExtras]...)
	return pool, nil
}

func candidatesOf(rows []db.Card) []wizardCandidate {
	out := make([]wizardCandidate, len(rows))
	for i, c := range rows {
		out[i] = wizardCandidate{card: c}
	}
	return out
}

// picksFromSynergy builds the picks pool from the commander's stored synergy
// sample (WIZ-043). It returns a nil view when there is no usable sample, and
// the caller falls back to popularity. A synergy lookup that fails is logged and
// treated the same way: the wizard must work without the synergy data.
func (a *API) picksFromSynergy(ctx context.Context, v wizardView, excluded map[string]bool) ([]wizardCandidate, *synergy.CommanderView, error) {
	if a.Synergy == nil {
		return nil, nil, nil
	}
	sv, err := a.Synergy.Commander(ctx, v.ld.commander.ID, synergy.MaxTopCards)
	if err != nil {
		log.Printf("wizard: synergy lookup for %s: %v", uuidString(v.ld.commander.ID), err)
		return nil, nil, nil
	}
	if sv.Sample == nil || len(sv.Cards) == 0 {
		return nil, nil, nil
	}
	var out []wizardCandidate
	for _, row := range sv.Cards {
		c := row.Card
		if excluded[uuidString(c.ID)] || frontFaceIsLand(c.TypeLine) || !identityWithin(c.ColorIdentity, v.identity) {
			continue
		}
		n := &wizardProposalSynergy{Decks: row.DeckCount, Inclusion: row.InclusionRate}
		if row.Synergy.Valid {
			s := row.Synergy.Float64
			n.Synergy = &s
		}
		out = append(out, wizardCandidate{card: c, syn: n})
	}
	return out, &sv, nil
}

func frontFaceIsLand(typeLine string) bool {
	front, _, _ := strings.Cut(typeLine, " // ")
	return strings.Contains(front, "Land")
}

// identityWithin reports whether a card's colour identity is a subset of the
// deck's.
func identityWithin(card, deck []string) bool {
	in := make(map[string]bool, len(deck))
	for _, c := range deck {
		in[c] = true
	}
	for _, c := range card {
		if !in[c] {
			return false
		}
	}
	return true
}

// attachPrices sets each candidate's price, in place, to the lowest known USD
// price across its non-digital printings (WIZ-031).
func (a *API) attachPrices(ctx context.Context, lists ...[]wizardCandidate) error {
	var ids []pgtype.UUID
	for _, list := range lists {
		for _, c := range list {
			ids = append(ids, c.card.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := a.Queries.ListCheapestUSD(ctx, ids)
	if err != nil {
		return err
	}
	byCard := make(map[string]float64, len(rows))
	for _, row := range rows {
		byCard[uuidString(row.CardID)] = row.Usd
	}
	for _, list := range lists {
		for i := range list {
			if p, ok := byCard[uuidString(list[i].card.ID)]; ok {
				price := p
				list[i].price = &price
			}
		}
	}
	return nil
}

// tutorIDs is the set of candidate card ids carrying the tutor tag or a
// descendant (WIZ-030).
func (a *API) tutorIDs(ctx context.Context, cands []wizardCandidate) (map[string]bool, error) {
	ids := make([]pgtype.UUID, len(cands))
	for i, c := range cands {
		ids[i] = c.card.ID
	}
	rows, err := a.Queries.FilterCardIDsByOracleTag(ctx, db.FilterCardIDsByOracleTagParams{Slug: "tutor", CardIds: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, id := range rows {
		out[uuidString(id)] = true
	}
	return out, nil
}

// ---- bracket admission context -------------------------------------------

// wizardAdmission holds what the bracket check needs to admit candidates: the
// working deck and the Spellbook flags of the deck's cards and the candidates.
type wizardAdmission struct {
	deck          deckwizard.Deck
	flags         map[string]db.SpellbookCardFlag
	combosChecked bool
}

// newWizardAdmission loads the deck's bracket view plus the combos that lie
// within the deck and the candidates (WIZ-021, WIZ-024).
func (a *API) newWizardAdmission(r *http.Request, v wizardView, cands []db.Card) (*wizardAdmission, error) {
	ctx := r.Context()
	cards, oracleIDs, commanders := bracketDeckCards(v.ld)
	for _, c := range cands {
		oracleIDs = append(oracleIDs, c.ScryfallOracleID)
	}
	flags, err := a.spellbookFlags(ctx, oracleIDs)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		applyFlags(&cards[i], flags)
	}
	combos, err := a.loadCombos(r, oracleIDs)
	if err != nil {
		return nil, err
	}
	available, err := a.Queries.ComboDataAvailable(ctx)
	if err != nil {
		return nil, err
	}
	return &wizardAdmission{
		deck:          deckwizard.Deck{Cards: cards, Commanders: commanders, Combos: combos},
		flags:         flags,
		combosChecked: available,
	}, nil
}

// card renders a candidate as a one-copy bracket card with its flags.
func (w *wizardAdmission) card(c db.Card) bracket.Card {
	bc := bracket.Card{OracleID: uuidString(c.ScryfallOracleID), Name: c.Name, Quantity: 1, GameChanger: c.IsGameChanger}
	applyFlags(&bc, w.flags)
	return bc
}

// ---- basics -------------------------------------------------------------

// wizardBasics returns the basic-land plan for a deck: how many basics it still
// needs to reach the land target and how they split across its colours. It
// writes nothing; the client applies the plan on the player's click (WIZ-050).
//
// @spec WIZ-004, WIZ-050, WIZ-051, WIZ-052, WIZ-053, WIZ-054, WIZ-114
func (a *API) wizardBasics(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	var req wizardBasicsRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err == nil && len(strings.TrimSpace(string(body))) > 0 {
		err = json.Unmarshal(body, &req)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}
	if req.LandTarget != nil && !deckwizard.ValidLandTarget(*req.LandTarget) {
		writeError(w, http.StatusBadRequest, "land_target must be between 20 and 60")
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	if ld.commander == nil {
		writeError(w, http.StatusUnprocessableEntity, wizardReasonText("no_commander"))
		return
	}
	v := newWizardView(ld)
	ctx := r.Context()
	if err := a.loadFastMana(ctx, &v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count the deck's fast mana")
		return
	}
	target := v.landTarget(req.LandTarget)
	needed := max(0, target-v.stats.LandCount)

	out := wizardBasicsResponse{
		LandTarget: target, LandsInDeck: v.stats.LandCount, BasicsNeeded: needed,
		Rows: []wizardBasicsRow{}, Missing: []string{},
	}
	for _, row := range deckwizard.SplitBasics(needed, v.identity, v.stats.ColorPips) {
		card, err := a.Queries.ResolveCardByName(ctx, row.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			out.Missing = append(out.Missing, row.Name)
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to look up basic lands")
			return
		}
		var print db.CardPrint
		havePrint := false
		if p, perr := a.Queries.GetNewestPrintForCard(ctx, card.ID); perr == nil {
			print, havePrint = p, true
		}
		inDeck := 0
		for _, e := range ld.entries {
			if e.CardID == card.ID && e.Board == "main" {
				inDeck += int(e.Quantity)
			}
		}
		out.Rows = append(out.Rows, wizardBasicsRow{
			Card: cardSummaryFrom(card, print, havePrint), Quantity: row.Quantity, InDeck: inDeck,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
