package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckrules"
)

var validBoards = map[string]bool{"command": true, "main": true, "maybe": true, "sideboard": true}

// countedBoard reports whether a board counts toward the 100-card deck and is
// checked for colour-identity and singleton violations (DECK-004, DECK-006);
// maybe and sideboard are staging areas that are never flagged. It mirrors the
// predicate deckrules.Validate applies internally.
func countedBoard(board string) bool { return board == "main" || board == "command" }

type deckJSON struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Format        string   `json:"format"`
	Bracket       *int32   `json:"bracket"`
	IsPublic      bool     `json:"is_public"`
	ColorIdentity []string `json:"color_identity"`
}

func deckJSONFrom(d db.Deck) deckJSON {
	return deckJSON{
		ID:            uuidString(d.ID),
		Name:          d.Name,
		Description:   d.Description,
		Format:        d.Format,
		Bracket:       int4Ptr(d.Bracket),
		IsPublic:      d.IsPublic,
		ColorIdentity: nonNil(d.ColorIdentity),
	}
}

type deckEntryJSON struct {
	EntryID                string          `json:"entry_id"`
	CardID                 string          `json:"card_id"`
	Name                   string          `json:"name"`
	ManaCost               *string         `json:"mana_cost"`
	ManaValue              float64         `json:"mana_value"`
	TypeLine               string          `json:"type_line"`
	ColorIdentity          []string        `json:"color_identity"`
	Quantity               int32           `json:"quantity"`
	Board                  string          `json:"board"`
	Category               *string         `json:"category"`
	ImageURIs              json.RawMessage `json:"image_uris"`
	Prices                 json.RawMessage `json:"prices"`
	SetCode                string          `json:"set_code"`
	CollectorNumber        string          `json:"collector_number"`
	ColorIdentityViolation bool            `json:"color_identity_violation"`
	OffendingColors        []string        `json:"offending_colors"`
	SingletonViolation     bool            `json:"singleton_violation"`
}

type deckDetailJSON struct {
	deckJSON
	Commander *cardSummary               `json:"commander"`
	Partner   *cardSummary               `json:"partner"`
	Boards    map[string][]deckEntryJSON `json:"boards"`
}

// loadedDeck is the fully resolved deck: its row, its entries, its assigned
// commander(s), and the legality report computed over all of it.
type loadedDeck struct {
	deck               db.Deck
	entries            []db.ListDeckCardEntriesRow
	commander          *db.Card
	partner            *db.Card
	commanderPrint     db.CardPrint
	partnerPrint       db.CardPrint
	haveCommanderPrint bool
	havePartnerPrint   bool
	report             deckrules.ValidationReport
}

// loadDeck resolves everything downstream of a decks row: its entries, its
// commander(s), the banlist overrides, and the legality report. It is shared by
// the owner detail view, the public view, and the validation endpoint so all
// three agree.
//
// @spec DECK-007, DECK-008
func (a *API) loadDeck(r *http.Request, deck db.Deck) (*loadedDeck, error) {
	ctx := r.Context()
	ld := &loadedDeck{deck: deck}

	entries, err := a.Queries.ListDeckCardEntries(ctx, deck.ID)
	if err != nil {
		return nil, err
	}
	ld.entries = entries

	overrides, err := a.Queries.ListBanlistOverrides(ctx)
	if err != nil {
		return nil, err
	}
	banned := make(map[string]bool, len(overrides))
	for _, o := range overrides {
		banned[strings.ToLower(o.CardName)] = o.Banned
	}
	overrideFor := func(name string) *bool {
		if v, ok := banned[strings.ToLower(name)]; ok {
			return &v
		}
		return nil
	}

	if deck.CommanderCardID.Valid {
		c, err := a.Queries.GetCardByID(ctx, deck.CommanderCardID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			ld.commander = &c
			if p, perr := a.Queries.GetNewestPrintForCard(ctx, c.ID); perr == nil {
				ld.commanderPrint, ld.haveCommanderPrint = p, true
			} else if !errors.Is(perr, pgx.ErrNoRows) {
				return nil, perr
			}
		}
	}
	if deck.PartnerCardID.Valid {
		c, err := a.Queries.GetCardByID(ctx, deck.PartnerCardID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			ld.partner = &c
			if p, perr := a.Queries.GetNewestPrintForCard(ctx, c.ID); perr == nil {
				ld.partnerPrint, ld.havePartnerPrint = p, true
			} else if !errors.Is(perr, pgx.ErrNoRows) {
				return nil, perr
			}
		}
	}

	// An imported Commander section is written as a command-board deck_cards row
	// without setting decks.commander_card_id. If that same card is later
	// assigned as commander/partner via the picker, it would be counted twice —
	// once from ld.commander/ld.partner and once from its own entry row. Drop the
	// duplicate command-board entry so every loadDeck consumer (validation,
	// detailJSON, stats, export) counts the assigned commander/partner exactly
	// once. A command-board row for a card that is not the assigned
	// commander/partner stays as a normal entry.
	if ld.commander != nil || ld.partner != nil {
		kept := ld.entries[:0]
		for _, e := range ld.entries {
			if e.Board == "command" &&
				((ld.commander != nil && e.CardID == ld.commander.ID) ||
					(ld.partner != nil && e.CardID == ld.partner.ID)) {
				continue
			}
			kept = append(kept, e)
		}
		ld.entries = kept
	}

	in := deckrules.ValidationInput{
		DeckColorIdentity: nonNil(deck.ColorIdentity),
	}
	if ld.commander != nil {
		f := cardFactsFrom(*ld.commander, overrideFor(ld.commander.Name))
		in.Commander = &f
		in.Entries = append(in.Entries, deckrules.Entry{Card: f, Board: "command", Quantity: 1})
	}
	if ld.partner != nil {
		f := cardFactsFrom(*ld.partner, overrideFor(ld.partner.Name))
		in.Partner = &f
		in.Entries = append(in.Entries, deckrules.Entry{Card: f, Board: "command", Quantity: 1})
	}
	for _, e := range ld.entries {
		in.Entries = append(in.Entries, deckrules.Entry{
			Card:     cardFactsFromEntry(e, overrideFor(e.Name)),
			Board:    e.Board,
			Quantity: int(e.Quantity),
		})
	}
	ld.report = deckrules.Validate(in)
	return ld, nil
}

func (ld *loadedDeck) detailJSON() deckDetailJSON {
	offending := map[string][]string{}
	for _, v := range ld.report.ColorIdentityViolations {
		offending[v.CardID] = v.Offending
	}
	singleton := map[string]bool{}
	for _, v := range ld.report.SingletonViolations {
		singleton[v.CardID] = true
	}

	boards := map[string][]deckEntryJSON{
		"command":   {},
		"main":      {},
		"maybe":     {},
		"sideboard": {},
	}

	addFlags := func(e deckEntryJSON) deckEntryJSON {
		if !countedBoard(e.Board) {
			return e
		}
		if o, ok := offending[e.CardID]; ok {
			e.ColorIdentityViolation = true
			e.OffendingColors = o
		}
		if singleton[e.CardID] {
			e.SingletonViolation = true
		}
		return e
	}

	if ld.commander != nil {
		boards["command"] = append(boards["command"],
			addFlags(entryJSONFromCard("commander", *ld.commander, ld.commanderPrint, ld.haveCommanderPrint)))
	}
	if ld.partner != nil {
		boards["command"] = append(boards["command"],
			addFlags(entryJSONFromCard("partner", *ld.partner, ld.partnerPrint, ld.havePartnerPrint)))
	}
	for _, e := range ld.entries {
		boards[e.Board] = append(boards[e.Board], addFlags(entryJSONFromRow(e)))
	}

	detail := deckDetailJSON{deckJSON: deckJSONFrom(ld.deck), Boards: boards}
	if ld.commander != nil {
		s := cardSummaryFrom(*ld.commander, ld.commanderPrint, ld.haveCommanderPrint)
		detail.Commander = &s
	}
	if ld.partner != nil {
		s := cardSummaryFrom(*ld.partner, ld.partnerPrint, ld.havePartnerPrint)
		detail.Partner = &s
	}
	return detail
}

func entryJSONFromRow(e db.ListDeckCardEntriesRow) deckEntryJSON {
	return deckEntryJSON{
		EntryID:         uuidString(e.EntryID),
		CardID:          uuidString(e.CardID),
		Name:            e.Name,
		ManaCost:        textPtr(e.ManaCost),
		ManaValue:       e.ManaValue,
		TypeLine:        e.TypeLine,
		ColorIdentity:   nonNil(e.ColorIdentity),
		Quantity:        e.Quantity,
		Board:           e.Board,
		Category:        textPtr(e.Category),
		ImageURIs:       rawOrNull(e.ImageUris),
		Prices:          rawOrNull(e.Prices),
		SetCode:         e.SetCode,
		CollectorNumber: e.CollectorNumber,
		OffendingColors: []string{},
	}
}

func entryJSONFromCard(entryID string, c db.Card, print db.CardPrint, havePrint bool) deckEntryJSON {
	e := deckEntryJSON{
		EntryID:         entryID,
		CardID:          uuidString(c.ID),
		Name:            c.Name,
		ManaCost:        textPtr(c.ManaCost),
		ManaValue:       c.ManaValue,
		TypeLine:        c.TypeLine,
		ColorIdentity:   nonNil(c.ColorIdentity),
		Quantity:        1,
		Board:           "command",
		ImageURIs:       json.RawMessage("null"),
		Prices:          json.RawMessage("null"),
		OffendingColors: []string{},
	}
	if havePrint {
		e.ImageURIs = rawOrNull(print.ImageUris)
		e.Prices = rawOrNull(print.Prices)
		e.SetCode = print.SetCode
		e.CollectorNumber = print.CollectorNumber
	}
	return e
}

// ---- handlers --------------------------------------------------------

// @spec DECK-001
func (a *API) listDecks(w http.ResponseWriter, r *http.Request) {
	uid, tok := callerOwner(r)
	decks, err := a.Queries.ListDecksForOwner(r.Context(), db.ListDecksForOwnerParams{UserID: uid, AnonToken: tok})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list decks")
		return
	}
	out := make([]deckJSON, 0, len(decks))
	for _, d := range decks {
		out = append(out, deckJSONFrom(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"decks": out})
}

// @spec DECK-001
func (a *API) createDeck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Untitled deck"
	}
	uid, tok := callerOwner(r)
	deck, err := a.Queries.CreateDeck(r.Context(), db.CreateDeckParams{UserID: uid, AnonToken: tok, Name: name})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create deck")
		return
	}
	writeJSON(w, http.StatusCreated, deckJSONFrom(deck))
}

// deckForOwner loads a decks row scoped to the caller, writing a 404 and
// returning ok=false when the deck does not exist or is not theirs (DECK-009).
func (a *API) deckForOwner(w http.ResponseWriter, r *http.Request) (db.Deck, bool) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "deck not found")
		return db.Deck{}, false
	}
	uid, tok := callerOwner(r)
	deck, err := a.Queries.GetDeckForOwner(r.Context(), db.GetDeckForOwnerParams{ID: id, UserID: uid, AnonToken: tok})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deck not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load deck")
		}
		return db.Deck{}, false
	}
	return deck, true
}

// @spec DECK-007
func (a *API) getDeck(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	writeJSON(w, http.StatusOK, ld.detailJSON())
}

// @spec DECK-011
func (a *API) updateDeck(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		IsPublic    *bool   `json:"is_public"`
		Bracket     *int32  `json:"bracket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	uid, tok := callerOwner(r)
	params := db.UpdateDeckMetaParams{
		Name:        deck.Name,
		Description: deck.Description,
		IsPublic:    deck.IsPublic,
		Bracket:     deck.Bracket,
		ID:          deck.ID,
		UserID:      uid,
		AnonToken:   tok,
	}
	if body.Name != nil {
		params.Name = strings.TrimSpace(*body.Name)
	}
	if body.Description != nil {
		params.Description = *body.Description
	}
	if body.IsPublic != nil {
		params.IsPublic = *body.IsPublic
	}
	if body.Bracket != nil {
		params.Bracket = pgtype.Int4{Int32: *body.Bracket, Valid: true}
	}

	updated, err := a.Queries.UpdateDeckMeta(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deck not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update deck")
		}
		return
	}
	writeJSON(w, http.StatusOK, deckJSONFrom(updated))
}

// @spec DECK-002, DECK-003
func (a *API) setCommander(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "deck not found")
		return
	}
	var body struct {
		CommanderCardID string `json:"commander_card_id"`
		PartnerCardID   string `json:"partner_card_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var params db.SetDeckCommanderParams
	params.ID = id
	params.UserID, params.AnonToken = callerOwner(r)
	params.ColorIdentity = []string{}

	var commander, partner *db.Card

	if strings.TrimSpace(body.CommanderCardID) != "" {
		cid, valid := parseUUID(body.CommanderCardID)
		if !valid {
			writeError(w, http.StatusBadRequest, "commander_card_id is not a valid id")
			return
		}
		c, err := a.Queries.GetCardByID(r.Context(), cid)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "commander card not found")
			return
		}
		if !c.CanBeCommander {
			writeError(w, http.StatusUnprocessableEntity, c.Name+" cannot be a commander")
			return
		}
		commander = &c
		params.CommanderCardID = cid
	}

	if strings.TrimSpace(body.PartnerCardID) != "" {
		pid, valid := parseUUID(body.PartnerCardID)
		if !valid {
			writeError(w, http.StatusBadRequest, "partner_card_id is not a valid id")
			return
		}
		c, err := a.Queries.GetCardByID(r.Context(), pid)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "partner card not found")
			return
		}
		partner = &c
		params.PartnerCardID = pid
	}

	var commanderFacts, partnerFacts *deckrules.CardFacts
	if commander != nil {
		f := cardFactsFrom(*commander, nil)
		commanderFacts = &f
	}
	if partner != nil {
		f := cardFactsFrom(*partner, nil)
		partnerFacts = &f
	}
	params.ColorIdentity = deckrules.ComputeDeckColorIdentity(commanderFacts, partnerFacts)
	if params.ColorIdentity == nil {
		params.ColorIdentity = []string{}
	}

	updated, err := a.Queries.SetDeckCommander(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deck not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to set commander")
		}
		return
	}

	ld, err := a.loadDeck(r, updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	writeJSON(w, http.StatusOK, ld.detailJSON())
}

// @spec DECK-004, DECK-005, DECK-006, DECK-009
func (a *API) addCard(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, "deck not found")
		return
	}
	var body struct {
		CardID   string `json:"card_id"`
		Board    string `json:"board"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	board := body.Board
	if board == "" {
		board = "main"
	}
	if !validBoards[board] {
		writeError(w, http.StatusBadRequest, "unknown board: "+board)
		return
	}
	cardID, valid := parseUUID(body.CardID)
	if !valid {
		writeError(w, http.StatusBadRequest, "card_id is not a valid id")
		return
	}

	uid, tok := callerOwner(r)
	params := db.AddDeckCardParams{
		CardID:    cardID,
		Quantity:  1,
		Board:     board,
		DeckID:    id,
		UserID:    uid,
		AnonToken: tok,
	}
	if strings.TrimSpace(body.Category) != "" {
		params.Category = pgtype.Text{String: body.Category, Valid: true}
	}

	entry, err := a.Queries.AddDeckCard(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deck not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to add card")
		}
		return
	}

	ciViol := false
	singletonViol := false
	offending := []string{}

	// DECK-004 / DECK-006: the colour-identity and singleton flags are scoped to
	// the main and command boards — the same boards GET /decks/{id}/validation
	// counts. A card staged on maybe/sideboard is never flagged, so the probe is
	// skipped entirely for those boards and the two endpoints stay in agreement.
	if countedBoard(board) {
		deck, err := a.Queries.GetDeckForOwner(r.Context(), db.GetDeckForOwnerParams{ID: id, UserID: uid, AnonToken: tok})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload deck")
			return
		}
		card, err := a.Queries.GetCardByID(r.Context(), cardID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload card")
			return
		}
		all, err := a.Queries.ListDeckCardEntries(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload deck cards")
			return
		}
		total := 0
		for _, e := range all {
			if uuidString(e.CardID) == uuidString(cardID) && countedBoard(e.Board) {
				total += int(e.Quantity)
			}
		}
		if total == 0 {
			total = int(entry.Quantity)
		}

		probe := deckrules.Validate(deckrules.ValidationInput{
			DeckColorIdentity: nonNil(deck.ColorIdentity),
			Entries: []deckrules.Entry{{
				Card:     cardFactsFrom(card, nil),
				Board:    board,
				Quantity: total,
			}},
		})
		ciViol = len(probe.ColorIdentityViolations) > 0
		if ciViol {
			offending = probe.ColorIdentityViolations[0].Offending
		}
		singletonViol = len(probe.SingletonViolations) > 0
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"entry_id":                 uuidString(entry.ID),
		"card_id":                  uuidString(entry.CardID),
		"board":                    entry.Board,
		"quantity":                 entry.Quantity,
		"color_identity_violation": ciViol,
		"offending_colors":         offending,
		"singleton_violation":      singletonViol,
	})
}

// @spec DECK-009, DECK-010
func (a *API) removeCard(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "deck id is not a valid id")
		return
	}
	cardID, ok := parseUUID(chi.URLParam(r, "cardId"))
	if !ok {
		writeError(w, http.StatusBadRequest, "card id is not a valid id")
		return
	}
	board := r.URL.Query().Get("board")
	if board == "" {
		board = "main"
	}
	uid, tok := callerOwner(r)
	rows, err := a.Queries.DeleteDeckCard(r.Context(), db.DeleteDeckCardParams{
		DeckID:    id,
		CardID:    cardID,
		Board:     board,
		UserID:    uid,
		AnonToken: tok,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove card")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "deck not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @spec DECK-008
func (a *API) getValidation(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate deck")
		return
	}
	writeJSON(w, http.StatusOK, ld.report)
}
