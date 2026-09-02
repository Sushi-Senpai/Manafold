package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// getPublicDeck serves the read-only public view of a deck. It is mounted
// outside the /api auth group, so an unknown or non-public id must resolve to a
// plain 404 and never a 401.
//
// @spec DECK-030, DECK-031
func (a *API) getPublicDeck(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "deck not found")
		return
	}
	deck, err := a.Queries.GetPublicDeck(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deck not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load deck")
		}
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	writeJSON(w, http.StatusOK, ld.detailJSON())
}
