package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/ai"
	"manafold-backend/internal/cardsearch"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckrules"
)

// suggestWant is how many suggestions the loop aims for (AI-020: "up to 10").
const suggestWant = 8

// candidatePoolSize is how many edhrec_rank-ordered cards seed the model's
// prompt before it may search for more (AI-011).
const candidatePoolSize = 40

// mirrorSearchLimit caps how many rows one search_cards tool call returns.
const mirrorSearchLimit = 25

// RegisterAIRoutes mounts the M4 AI endpoints. It is called inside the
// authenticated /api group; each handler additionally rejects an anonymous
// draft caller (AI-033).
//
// @spec AI-020, AI-021, PLATFORM-005
func (a *API) RegisterAIRoutes(r chi.Router) {
	r.Post("/decks/{id}/suggestions", a.suggestCards)
	r.Post("/decks/{id}/cards/{cardId}/explain", a.explainCard)
}

// aiGateCaller runs the caller-level gates every AI endpoint shares, in order:
// 503 if the assistant is disabled; 403 if the caller is an anonymous draft
// (AI-033). It returns the authenticated user id and ok=true only when both
// pass. Deck ownership (DECK-009, via deckForOwner) and then the spend/quota
// gates (aiGateQuota) run after this, in that order, so a caller learns nothing
// about global spend or their own quota for a deck they cannot access.
func (a *API) aiGateCaller(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if a.AI == nil || !a.AI.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "AI features are not configured")
		return pgtype.UUID{}, false
	}

	uid, tok := callerOwner(r)
	if !uid.Valid {
		if tok.Valid {
			writeError(w, http.StatusForbidden, "sign in to use AI features")
		} else {
			writeError(w, http.StatusUnauthorized, "not authenticated")
		}
		return pgtype.UUID{}, false
	}
	return uid, true
}

// aiGateQuota runs the cost gates after deck ownership is confirmed, in order:
// 503 if the global month-to-date ceiling is hit (AI-032); 429 if the caller is
// at the feature's daily limit (AI-031). It returns ok=true only when both pass.
func (a *API) aiGateQuota(w http.ResponseWriter, r *http.Request, feature string, dailyLimit int, uid pgtype.UUID) bool {
	ctx := r.Context()
	if a.AIMonthlySpendMicros > 0 {
		spent, err := a.Queries.SumAICostMicrosSince(ctx, firstOfMonth())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check AI spend")
			return false
		}
		if spent >= a.AIMonthlySpendMicros {
			writeError(w, http.StatusServiceUnavailable, "AI features are temporarily unavailable (monthly limit reached)")
			return false
		}
	}

	used, err := a.Queries.CountAIFeatureCallsToday(ctx, db.CountAIFeatureCallsTodayParams{UserID: uid, Feature: feature})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check AI quota")
		return false
	}
	if int(used) >= dailyLimit {
		writeError(w, http.StatusTooManyRequests, "daily AI limit reached; try again tomorrow")
		return false
	}
	return true
}

// meterIfBilled records usage for any model call that returned token counts —
// even one that ultimately errored (no parseable suggestions, a truncated
// payload) or whose result a later step rejects. A completed round-trip has
// incurred cost and must count against the daily quota (AI-031) and the monthly
// ceiling (AI-032); a call that never reached the model carries a zero Usage and
// is skipped, so a disabled assistant or a transport error before any response
// never burns quota (AI-034).
func (a *API) meterIfBilled(r *http.Request, uid pgtype.UUID, feature string, u ai.Usage) {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return
	}
	a.recordUsage(r, uid, feature, u)
}

// recordUsage writes the ai_usage row (AI-030). A write failure is logged and
// swallowed: it must not fail the response the user already earned, but a
// persistent failure silently starves the daily quota (AI-031) and the monthly
// ceiling (AI-032), so it is logged loudly rather than dropped.
func (a *API) recordUsage(r *http.Request, uid pgtype.UUID, feature string, u ai.Usage) {
	if err := a.Queries.RecordAIUsage(r.Context(), db.RecordAIUsageParams{
		UserID:       uid,
		Feature:      feature,
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		CostMicros:   u.CostMicros,
	}); err != nil {
		log.Printf("ai: failed to record %s usage for user %s: %v", feature, uuidString(uid), err)
	}
}

// @spec AI-011, AI-013, AI-020, AI-024, AI-030, AI-031, AI-032, AI-033, AI-034, AI-035
func (a *API) suggestCards(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.aiGateCaller(w, r)
	if !ok {
		return
	}
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	if !a.aiGateQuota(w, r, "suggest", a.AISuggestDailyLimit, uid) {
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	if ld.commander == nil {
		writeError(w, http.StatusUnprocessableEntity, "assign a commander before requesting suggestions")
		return
	}

	ctx := r.Context()
	identity := nonNil(deck.ColorIdentity)

	// Exclude everything already in the deck plus the commander(s) from the pool.
	exclude := []pgtype.UUID{ld.commander.ID}
	inDeck := []string{ld.commander.Name}
	if ld.partner != nil {
		exclude = append(exclude, ld.partner.ID)
		inDeck = append(inDeck, ld.partner.Name)
	}
	for _, e := range ld.entries {
		exclude = append(exclude, e.CardID)
		inDeck = append(inDeck, e.Name)
	}

	pool, err := a.Queries.ListSuggestionCandidates(ctx, db.ListSuggestionCandidatesParams{
		DeckIdentity: identity,
		ExcludeIds:   exclude,
		Lim:          int32(candidatePoolSize),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build candidate pool")
		return
	}

	candidates := make([]ai.CardRef, 0, len(pool))
	for _, c := range pool {
		candidates = append(candidates, cardRefFromCard(c))
	}

	res, err := a.AI.Suggest(ctx, ai.SuggestRequest{
		CommanderName: ld.commander.Name,
		CommanderText: ld.commander.OracleText,
		DeckIdentity:  identity,
		InDeck:        inDeck,
		Candidates:    candidates,
		Want:          suggestWant,
		Search:        a.mirrorSearch(identity),
	})
	a.meterIfBilled(r, uid, "suggest", res.Usage)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "AI features are not configured")
			return
		}
		writeError(w, http.StatusBadGateway, "AI suggestion failed")
		return
	}

	kept, dropped, err := a.gateSuggestions(ctx, res.Suggestions, ld)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate suggestions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"suggestions": kept,
		"dropped":     dropped,
		"model":       res.Usage.Model,
	})
}

// @spec AI-021, AI-030, AI-031, AI-032, AI-033, AI-034, AI-035
func (a *API) explainCard(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.aiGateCaller(w, r)
	if !ok {
		return
	}
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	if !a.aiGateQuota(w, r, "explain", a.AIExplainDailyLimit, uid) {
		return
	}
	cardID, ok := parseUUID(chi.URLParam(r, "cardId"))
	if !ok {
		writeError(w, http.StatusBadRequest, "card id is not a valid id")
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}

	// The card must be on the deck's main or command board — the explain blurb
	// is about a card the deck actually runs.
	name, text := "", ""
	if ld.commander != nil && ld.commander.ID == cardID {
		name, text = ld.commander.Name, ld.commander.OracleText
	} else if ld.partner != nil && ld.partner.ID == cardID {
		name, text = ld.partner.Name, ld.partner.OracleText
	} else {
		for _, e := range ld.entries {
			if e.CardID == cardID && (e.Board == "main" || e.Board == "command") {
				name, text = e.Name, e.OracleText
				break
			}
		}
	}
	if name == "" {
		writeError(w, http.StatusNotFound, "that card is not on this deck's main or command board")
		return
	}

	commanderName, commanderText := "", ""
	if ld.commander != nil {
		commanderName, commanderText = ld.commander.Name, ld.commander.OracleText
	}

	res, err := a.AI.Explain(r.Context(), ai.ExplainRequest{
		CommanderName: commanderName,
		CommanderText: commanderText,
		CardName:      name,
		CardText:      text,
		DeckIdentity:  nonNil(deck.ColorIdentity),
	})
	a.meterIfBilled(r, uid, "explain", res.Usage)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "AI features are not configured")
			return
		}
		writeError(w, http.StatusBadGateway, "AI explanation failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"card_id":     uuidString(cardID),
		"explanation": res.Text,
		"model":       res.Usage.Model,
	})
}

// gateSuggestions is the anti-hallucination gate (AI-010, AI-013): resolve every
// model-named card in the mirror, re-validate it with internal/deckrules for the
// deck's colour identity, and drop anything that does not resolve, is banned, is
// outside the identity, is already in the deck, or repeats an earlier survivor.
// Nothing is substituted for a dropped card; the drop count is returned. A
// failure loading the manual banlist overrides aborts the gate with an error
// rather than degrading it to "no overrides", so a transient DB error can never
// let a manually-banned card through (AI-010).
//
// @spec AI-010, AI-013
func (a *API) gateSuggestions(ctx context.Context, suggestions []ai.Suggestion, ld *loadedDeck) ([]map[string]any, int, error) {
	loadOverrides := a.Queries.ListBanlistOverrides
	if a.overridesLoader != nil {
		loadOverrides = a.overridesLoader
	}
	overrides, err := loadOverrides(ctx)
	if err != nil {
		return nil, 0, err
	}
	banned := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		banned[strings.ToLower(o.CardName)] = o.Banned
	}
	overrideFor := func(cardName string) *bool {
		if v, ok := banned[strings.ToLower(cardName)]; ok {
			return &v
		}
		return nil
	}

	inDeck := map[string]bool{uuidString(ld.commander.ID): true}
	if ld.partner != nil {
		inDeck[uuidString(ld.partner.ID)] = true
	}
	for _, e := range ld.entries {
		inDeck[uuidString(e.CardID)] = true
	}

	identity := nonNil(ld.deck.ColorIdentity)
	kept := []map[string]any{}
	seen := map[string]bool{}
	dropped := 0

	for _, s := range suggestions {
		card, err := a.Queries.ResolveCardByName(ctx, strings.TrimSpace(s.Name))
		if err != nil {
			dropped++
			continue
		}
		key := uuidString(card.ID)
		if inDeck[key] || seen[key] {
			dropped++
			continue
		}

		report := deckrules.Validate(deckrules.ValidationInput{
			DeckColorIdentity: identity,
			Entries: []deckrules.Entry{{
				Card:     cardFactsFrom(card, overrideFor(card.Name)),
				Board:    "main",
				Quantity: 1,
			}},
		})
		if len(report.ColorIdentityViolations) > 0 || len(report.BanlistViolations) > 0 {
			dropped++
			continue
		}

		seen[key] = true
		var print db.CardPrint
		havePrint := false
		if p, perr := a.Queries.GetNewestPrintForCard(ctx, card.ID); perr == nil {
			print, havePrint = p, true
		}
		kept = append(kept, map[string]any{
			"card":   cardSummaryFrom(card, print, havePrint),
			"reason": strings.TrimSpace(s.Reason),
		})
	}
	return kept, dropped, nil
}

// mirrorSearch backs the search_cards tool: it parses Manafold's mini-Scryfall
// syntax and runs it over the card table with the deck's colour-identity subset
// and the non-banned filter forced on, so the model can never see an illegal
// candidate (AI-012).
//
// @spec AI-012
func (a *API) mirrorSearch(identity []string) ai.SearchFunc {
	return func(ctx context.Context, query string) ([]ai.CardRef, error) {
		parsed, err := cardsearch.Parse(query)
		if err != nil {
			return nil, err
		}
		where, args, next := parsed.WhereSQL(1)
		sql := fmt.Sprintf(`
SELECT name, mana_cost, type_line, oracle_text, color_identity, edhrec_rank
FROM cards c
WHERE (%s)
  AND c.color_identity <@ $%d::text[]
  AND COALESCE(c.legalities->>'commander', '') <> 'banned'
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT %d`, where, next, mirrorSearchLimit)
		args = append(args, identity)

		rows, err := a.Pool.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var out []ai.CardRef
		for rows.Next() {
			var (
				ref      ai.CardRef
				manaCost pgtype.Text
				rank     pgtype.Int4
			)
			if err := rows.Scan(&ref.Name, &manaCost, &ref.TypeLine, &ref.OracleText, &ref.ColorIdentity, &rank); err != nil {
				return nil, err
			}
			ref.ManaCost = manaCost.String
			if rank.Valid {
				v := int(rank.Int32)
				ref.EdhrecRank = &v
			}
			out = append(out, ref)
		}
		return out, rows.Err()
	}
}

// cardRefFromCard renders a cards row as the minimal shape the assistant sees.
func cardRefFromCard(c db.Card) ai.CardRef {
	ref := ai.CardRef{
		Name:          c.Name,
		ManaCost:      c.ManaCost.String,
		TypeLine:      c.TypeLine,
		OracleText:    c.OracleText,
		ColorIdentity: nonNil(c.ColorIdentity),
	}
	if c.EdhrecRank.Valid {
		v := int(c.EdhrecRank.Int32)
		ref.EdhrecRank = &v
	}
	return ref
}

// firstOfMonth is midnight on the first day of the current month, the lower
// bound for the global spend-ceiling sum (AI-032).
func firstOfMonth() pgtype.Date {
	now := time.Now().UTC()
	return pgtype.Date{Time: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// AISpendMicros converts a USD ceiling to the micro-USD unit ai_usage stores.
// server.New calls it once to fill API.AIMonthlySpendMicros.
func AISpendMicros(usd float64) int64 {
	return int64(usd * 1_000_000)
}
