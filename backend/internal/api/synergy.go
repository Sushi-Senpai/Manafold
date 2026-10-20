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

// RegisterSynergyRoutes mounts the commander-synergy endpoints. It is called
// inside the authenticated /api group.
//
// @spec SYN-030, SYN-040, PLATFORM-005
func (a *API) RegisterSynergyRoutes(r chi.Router) {
	r.Get("/synergy/commanders/{cardId}", a.getCommanderSynergy)
	r.Post("/synergy/commanders/{cardId}/ensure", a.ensureCommanderSynergy)
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

type synergyCommander struct {
	CardID        string `json:"card_id"`
	Name          string `json:"name"`
	ColorIdentity string `json:"color_identity"`
}

// synergyStatusResponse is the ensure answer: the status and, when a sample is
// stored, its metadata.
type synergyStatusResponse struct {
	Status string         `json:"status"`
	Reason *string        `json:"reason"`
	Sample *synergySample `json:"sample"`
}

type commanderSynergyResponse struct {
	Status      string            `json:"status"`
	Reason      *string           `json:"reason"`
	Commander   synergyCommander  `json:"commander"`
	Sample      *synergySample    `json:"sample"`
	Cards       []synergyCard     `json:"cards"`
	Attribution map[string]string `json:"attribution"`
}

// synergyUnavailable answers when the API was built without a synergy service.
func (a *API) synergyUnavailable(w http.ResponseWriter) bool {
	if a.Synergy == nil {
		writeError(w, http.StatusServiceUnavailable, "synergy is not configured")
		return true
	}
	return false
}

// ensureCommanderSynergy queues gathering for a commander that has no fresh
// synergy data and answers with its status immediately; it never waits on
// Archidekt (SYN-030, SYN-031, SYN-032, SYN-042).
//
// @spec SYN-030, SYN-042
func (a *API) ensureCommanderSynergy(w http.ResponseWriter, r *http.Request) {
	if a.synergyUnavailable(w) {
		return
	}
	cardID, ok := parseUUID(chi.URLParam(r, "cardId"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	v, err := a.Synergy.Ensure(r.Context(), cardID)
	if errors.Is(err, synergy.ErrNoCard) {
		writeError(w, http.StatusNotFound, "card not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "synergy lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, synergyStatusResponse{
		Status: string(v.State),
		Reason: reasonPtr(v.Reason),
		Sample: sampleFrom(v.Sample),
	})
}

// getCommanderSynergy serves a commander's synergy status and, when any sample
// is stored, its top cards from the local database only — never Archidekt —
// with the sample size and the Archidekt attribution (SYN-040, SYN-041). A card
// id the mirror does not know is 404 (SYN-042).
//
// @spec SYN-040, SYN-041, SYN-042
func (a *API) getCommanderSynergy(w http.ResponseWriter, r *http.Request) {
	if a.synergyUnavailable(w) {
		return
	}
	cardID, ok := parseUUID(chi.URLParam(r, "cardId"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	v, err := a.Synergy.Commander(r.Context(), cardID, limit)
	if errors.Is(err, synergy.ErrNoCard) {
		writeError(w, http.StatusNotFound, "card not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "synergy lookup failed")
		return
	}

	out := commanderSynergyResponse{
		Status: string(v.State),
		Reason: reasonPtr(v.Reason),
		Commander: synergyCommander{
			CardID:        uuidString(cardID),
			Name:          v.Card.Name,
			ColorIdentity: synergy.CanonicalIdentity(v.Card.Identity),
		},
		Sample:      sampleFrom(v.Sample),
		Cards:       make([]synergyCard, 0, len(v.Cards)),
		Attribution: map[string]string{"text": synergy.AttributionText, "url": synergy.AttributionURL},
	}
	for _, row := range v.Cards {
		out.Cards = append(out.Cards, synergyCardFrom(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func reasonPtr(r string) *string {
	if r == "" {
		return nil
	}
	return &r
}

// sampleFrom maps a stored sample to its API form. An identity whose baseline
// is missing or empty has no baseline to report.
func sampleFrom(s *db.GetSynergySampleRow) *synergySample {
	if s == nil {
		return nil
	}
	out := &synergySample{
		DeckCount:         s.DeckCount,
		DuplicatesSkipped: s.DuplicatesSkipped,
		FetchedAt:         s.FetchedAt.Time,
	}
	if s.BaselineDeckCount.Valid && s.BaselineDeckCount.Int32 > 0 {
		n := s.BaselineDeckCount.Int32
		t := s.BaselineFetchedAt.Time
		out.BaselineDeckCount = &n
		out.BaselineFetchedAt = &t
	}
	return out
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
