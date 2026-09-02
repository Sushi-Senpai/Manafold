package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/cardsearch"
)

// searchPageSize is the fixed page size for GET /api/cards/search (card-data
// design: "Page size 60").
const searchPageSize = 60

// @spec CARD-020
func (a *API) autocompleteCards(w http.ResponseWriter, r *http.Request) {
	prefix := strings.TrimSpace(r.URL.Query().Get("q"))
	if prefix == "" {
		writeJSON(w, http.StatusOK, map[string]any{"names": []string{}})
		return
	}
	names, err := a.Queries.AutocompleteCardNames(r.Context(), prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "autocomplete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"names": nonNil(names)})
}

// @spec CARD-021, CARD-022, CARD-023, CARD-024
func (a *API) searchCards(w http.ResponseWriter, r *http.Request) {
	rawQuery := r.URL.Query().Get("q")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 0 {
		page = 0
	}

	parsed, err := cardsearch.Parse(rawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	where, args, next := parsed.WhereSQL(1)
	ctx := r.Context()

	var total int
	countSQL := "SELECT count(*) FROM cards c WHERE " + where
	if err := a.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	listSQL := fmt.Sprintf(`
SELECT c.id, c.name, c.mana_cost, c.mana_value, c.type_line, c.oracle_text,
       c.colors, c.color_identity, c.keywords, c.can_be_commander, c.edhrec_rank, c.layout,
       p.image_uris, p.prices,
       COALESCE(p.set_code, '') AS set_code,
       COALESCE(p.collector_number, '') AS collector_number
FROM cards c
LEFT JOIN LATERAL (
    SELECT image_uris, prices, set_code, collector_number
    FROM card_prints
    WHERE card_id = c.id
    ORDER BY released_at DESC NULLS LAST
    LIMIT 1
) p ON true
WHERE %s
ORDER BY c.edhrec_rank ASC NULLS LAST, c.name ASC
LIMIT $%d OFFSET $%d`, where, next, next+1)

	listArgs := append(append([]any{}, args...), searchPageSize, page*searchPageSize)
	rows, err := a.Pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()

	cards := []cardSummary{}
	for rows.Next() {
		var (
			cs                cardSummary
			id                pgtype.UUID
			manaCost          pgtype.Text
			edhrecRank        pgtype.Int4
			imageURIs, prices []byte
		)
		if err := rows.Scan(
			&id, &cs.Name, &manaCost, &cs.ManaValue, &cs.TypeLine, &cs.OracleText,
			&cs.Colors, &cs.ColorIdentity, &cs.Keywords, &cs.CanBeCommander, &edhrecRank, &cs.Layout,
			&imageURIs, &prices, &cs.SetCode, &cs.CollectorNumber,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
		cs.ID = uuidString(id)
		cs.ManaCost = textPtr(manaCost)
		cs.EdhrecRank = int4Ptr(edhrecRank)
		cs.Colors = nonNil(cs.Colors)
		cs.ColorIdentity = nonNil(cs.ColorIdentity)
		cs.Keywords = nonNil(cs.Keywords)
		cs.ImageURIs = rawOrNull(imageURIs)
		cs.Prices = rawOrNull(prices)
		cards = append(cards, cs)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cards":    cards,
		"total":    total,
		"page":     page,
		"has_more": page*searchPageSize+len(cards) < total,
	})
}
