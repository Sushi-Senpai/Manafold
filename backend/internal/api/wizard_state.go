package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	db "manafold-backend/internal/db/generated"
)

// wizardState is the deck wizard's saved answers: what the first slice already
// asks, and nothing the deck itself holds (WIZ-092). The server validates it and
// stores it as given.
type wizardState struct {
	Step            string             `json:"step"`
	Restrictions    wizardRestrictions `json:"restrictions"`
	Fixing          string             `json:"fixing"`
	IncludeFetch    bool               `json:"include_fetch"`
	Utility         bool               `json:"utility"`
	LandTarget      *int               `json:"land_target"`
	RejectedCardIDs []string           `json:"rejected_card_ids"`
}

type wizardStateResponse struct {
	Saved     bool         `json:"saved"`
	State     *wizardState `json:"state"`
	UpdatedAt *time.Time   `json:"updated_at"`
}

var (
	wizardStateSteps  = map[string]bool{"setup": true, "fixing": true, "utility": true, "picks": true, "basics": true}
	wizardStateFixing = map[string]bool{"include": true, "basics": true}
)

// validate reports why a saved state is refused, or "" (WIZ-102).
func (s wizardState) validate() string {
	if !wizardStateSteps[s.Step] {
		return "step must be one of setup, fixing, utility, picks, basics"
	}
	if !wizardStateFixing[s.Fixing] {
		return "fixing must be include or basics"
	}
	return validateWizardAnswers(s.LandTarget, s.Restrictions, s.RejectedCardIDs)
}

func wizardStateJSON(raw json.RawMessage, at time.Time) (wizardStateResponse, error) {
	var st wizardState
	if err := json.Unmarshal(raw, &st); err != nil {
		return wizardStateResponse{}, err
	}
	if st.RejectedCardIDs == nil {
		st.RejectedCardIDs = []string{}
	}
	return wizardStateResponse{Saved: true, State: &st, UpdatedAt: &at}, nil
}

// getWizardState returns the deck's saved wizard answers. It needs neither a
// model nor a sign-in.
//
// @spec WIZ-092, WIZ-100
func (a *API) getWizardState(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	row, err := a.Queries.GetWizardState(r.Context(), deck.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, wizardStateResponse{})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load wizard state")
		return
	}
	resp, err := wizardStateJSON(row.State, row.UpdatedAt.Time)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read wizard state")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// putWizardState replaces the deck's saved wizard answers.
//
// @spec WIZ-092, WIZ-101, WIZ-102
func (a *API) putWizardState(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	var st wizardState
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&st); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}
	if msg := st.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if st.RejectedCardIDs == nil {
		st.RejectedCardIDs = []string{}
	}
	raw, err := json.Marshal(st)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save wizard state")
		return
	}
	row, err := a.Queries.UpsertWizardState(r.Context(), db.UpsertWizardStateParams{DeckID: deck.ID, State: raw})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save wizard state")
		return
	}
	resp, err := wizardStateJSON(row.State, row.UpdatedAt.Time)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read wizard state")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// deleteWizardState forgets the deck's saved answers. The deck's cards, bracket
// and archetype are untouched.
//
// @spec WIZ-103
func (a *API) deleteWizardState(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	if err := a.Queries.DeleteWizardState(r.Context(), deck.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset wizard state")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
