package api

import (
	"encoding/json"
	"net/http"

	"manafold-backend/internal/deckroles"
	"manafold-backend/internal/deckstats"
	"manafold-backend/internal/decktemplates"
)

// roleClassifier counts template role rows. Every caller goes through the
// deckroles.Classifier interface, so swapping in a tag-backed classifier is a
// one-line change here (DECK-106).
var roleClassifier deckroles.Classifier = deckroles.OracleClassifier{}

type deckTemplatesJSON struct {
	Version   int                      `json:"version"`
	Templates []decktemplates.Template `json:"templates"`
}

// listDeckTemplates serves the authoritative template data verbatim, so the
// builder panel and the later wizard read the same source (DECK-100).
//
// @spec DECK-100
func (a *API) listDeckTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, deckTemplatesJSON{
		Version:   decktemplates.Version,
		Templates: decktemplates.All(),
	})
}

type templateFitJSON struct {
	Archetype       *string                `json:"archetype"`
	TemplateVersion int                    `json:"template_version"`
	Bracket         *int32                 `json:"bracket"`
	Rows            []decktemplates.RowFit `json:"rows"`
}

// getTemplateFit reads the deck's main board — the 99 non-commander cards —
// against its archetype's template. Type rows reuse deckstats' primary-type
// counts (DECK-105); role rows come from the role classifier (DECK-106). A deck
// with no archetype (or one no longer in the template set) gets no rows.
//
// @spec DECK-104, DECK-105, DECK-106, DECK-108
func (a *API) getTemplateFit(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	out := templateFitJSON{
		TemplateVersion: decktemplates.Version,
		Bracket:         int4Ptr(deck.Bracket),
		Rows:            []decktemplates.RowFit{},
	}
	tpl, ok := decktemplates.Lookup(deck.Archetype.String)
	if !deck.Archetype.Valid || !ok {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Archetype = textPtr(deck.Archetype)

	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	var stats []deckstats.CardStat
	var roles []deckroles.Entry
	for _, e := range ld.entries {
		if e.Board != "main" {
			continue
		}
		stats = append(stats, deckstats.CardStat{
			TypeLine:  e.TypeLine,
			ManaValue: e.ManaValue,
			Quantity:  int(e.Quantity),
			IsLand:    isBasicLand(e.TypeLine),
		})
		roles = append(roles, deckroles.Entry{
			Card:     roleCardOf(e.TypeLine, e.OracleText, e.Keywords, e.CardFaces),
			Quantity: int(e.Quantity),
		})
	}

	bracket := 0
	if deck.Bracket.Valid {
		bracket = int(deck.Bracket.Int32)
	}
	counts := decktemplates.Counts(
		deckstats.Analyze(stats).TypeCounts,
		deckroles.Count(roleClassifier, roles),
	)
	out.Rows = decktemplates.Evaluate(tpl, counts, bracket)
	writeJSON(w, http.StatusOK, out)
}

// roleCardOf is the classifier input for a card's Oracle fields. A malformed
// card_faces blob just leaves the card unclassified by face; the top-level text
// still applies.
func roleCardOf(typeLine, oracleText string, keywords []string, cardFaces []byte) deckroles.Card {
	var faces []deckroles.Face
	if len(cardFaces) > 0 {
		_ = json.Unmarshal(cardFaces, &faces)
	}
	return deckroles.Card{TypeLine: typeLine, OracleText: oracleText, Keywords: keywords, Faces: faces}
}
