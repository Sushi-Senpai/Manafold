package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/synergy"
)

// RegisterSynergyRoutes mounts the commander-synergy read endpoint. It is
// called inside the authenticated /api group.
//
// @spec SYN-040, PLATFORM-005
func (a *API) RegisterSynergyRoutes(r chi.Router) {
	r.Get("/synergy/commanders/{cardId}", a.getCommanderSynergy)
}

type synergyCard struct {
	Card      cardSummary `json:"card"`
	Decks     int32       `json:"decks"`
	Inclusion float64     `json:"inclusion"`
	Baseline  *float64    `json:"baseline"`
	Synergy   *float64    `json:"synergy"`
}

type synergySample struct {
	DeckCount         int32      `json:"deck_count"`
	DuplicatesSkipped int32      `json:"duplicates_skipped"`
	FetchedAt         time.Time  `json:"fetched_at"`
	BaselineDeckCount *int32     `json:"baseline_deck_count"`
	BaselineFetchedAt *time.Time `json:"baseline_fetched_at"`
}

type commanderSynergyResponse struct {
	Commander struct {
		CardID        string `json:"card_id"`
		Name          string `json:"name"`
		ColorIdentity string `json:"color_identity"`
	} `json:"commander"`
	Sample      synergySample     `json:"sample"`
	Cards       []synergyCard     `json:"cards"`
	Attribution map[string]string `json:"attribution"`
}

// getCommanderSynergy serves a commander's top synergy cards from the local
// database only — never Archidekt — with the sample size and the Archidekt
// attribution (SYN-040, SYN-041). A commander with no sample is 404 (SYN-042).
//
// @spec SYN-040, SYN-041, SYN-042
func (a *API) getCommanderSynergy(w http.ResponseWriter, r *http.Request) {
	cardID, ok := parseUUID(chi.URLParam(r, "cardId"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	res, err := synergy.TopCards(r.Context(), a.Queries, cardID, limit)
	if errors.Is(err, synergy.ErrNoSample) {
		writeError(w, http.StatusNotFound, "no synergy data for this commander")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "synergy lookup failed")
		return
	}

	var out commanderSynergyResponse
	out.Commander.CardID = uuidString(cardID)
	out.Commander.Name = res.Sample.CommanderName
	out.Commander.ColorIdentity = res.Sample.ColorIdentity
	out.Sample = synergySample{
		DeckCount:         res.Sample.DeckCount,
		DuplicatesSkipped: res.Sample.DuplicatesSkipped,
		FetchedAt:         res.Sample.FetchedAt.Time,
	}
	// An identity whose baseline is missing or empty has no baseline to report.
	if res.Sample.BaselineDeckCount.Valid && res.Sample.BaselineDeckCount.Int32 > 0 {
		n := res.Sample.BaselineDeckCount.Int32
		t := res.Sample.BaselineFetchedAt.Time
		out.Sample.BaselineDeckCount = &n
		out.Sample.BaselineFetchedAt = &t
	}
	out.Cards = make([]synergyCard, 0, len(res.Cards))
	for _, row := range res.Cards {
		out.Cards = append(out.Cards, synergyCardFrom(row))
	}
	out.Attribution = map[string]string{"text": synergy.AttributionText, "url": synergy.AttributionURL}
	writeJSON(w, http.StatusOK, out)
}

func synergyCardFrom(row db.ListTopSynergyCardsRow) synergyCard {
	cs := cardSummaryFrom(row.Card, db.CardPrint{}, false)
	cs.ImageURIs = rawOrNull(row.ImageUris)
	cs.Prices = rawOrNull(row.Prices)
	cs.SetCode = row.SetCode
	cs.CollectorNumber = row.CollectorNumber
	sc := synergyCard{Card: cs, Decks: row.DeckCount, Inclusion: row.InclusionRate}
	if row.BaselineRate.Valid {
		v := row.BaselineRate.Float64
		sc.Baseline = &v
	}
	if row.Synergy.Valid {
		v := row.Synergy.Float64
		sc.Synergy = &v
	}
	return sc
}
