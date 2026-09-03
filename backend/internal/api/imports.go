package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckio"
)

// RegisterImportRoutes mounts the import/export endpoints for a deck. It is
// called inside the authenticated /api group, beside RegisterDeckRoutes
// (PLATFORM-005) — import/export is its own arrow segment.
//
// @spec PORT-001, PORT-006, PORT-007, PLATFORM-005
func (a *API) RegisterImportRoutes(r chi.Router) {
	r.Post("/decks/{id}/import", a.parseImport)
	r.Post("/decks/{id}/import/{importId}/apply", a.applyImport)
	r.Get("/decks/{id}/export", a.exportDeck)
}

// resolvedLineJSON is a parsed line that matched a card in the mirror.
type resolvedLineJSON struct {
	CardID   string `json:"card_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Board    string `json:"board"`
	Category string `json:"category,omitempty"`
}

// unresolvedLineJSON is a parsed line whose name matched no card. It is never
// dropped — the user is shown exactly what did not resolve (PORT-004).
type unresolvedLineJSON struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Board    string `json:"board"`
	Raw      string `json:"raw"`
}

type importResponse struct {
	ImportID   string               `json:"import_id"`
	Resolved   []resolvedLineJSON   `json:"resolved"`
	Unresolved []unresolvedLineJSON `json:"unresolved"`
	Rejected   []string             `json:"rejected"`
}

// resolveLines walks a parsed decklist, matching each line's name against the
// mirror (exact case-insensitively, or one face of a split/DFC card — PORT-005),
// and splits the result into resolved and unresolved lines.
func (a *API) resolveLines(ctx context.Context, parsed deckio.ParseResult) (resolved []resolvedLineJSON, unresolved []unresolvedLineJSON) {
	for _, l := range parsed.Lines {
		card, err := a.Queries.ResolveCardByName(ctx, l.Name)
		if err != nil {
			unresolved = append(unresolved, unresolvedLineJSON{
				Name: l.Name, Quantity: l.Quantity, Board: l.Board, Raw: l.Raw,
			})
			continue
		}
		resolved = append(resolved, resolvedLineJSON{
			CardID:   uuidString(card.ID),
			Name:     card.Name,
			Quantity: l.Quantity,
			Board:    l.Board,
			Category: l.Category,
		})
	}
	return resolved, unresolved
}

// @spec PORT-001, PORT-002, PORT-003, PORT-004, PORT-005
func (a *API) parseImport(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	var body struct {
		SourceFormat string `json:"source_format"`
		RawText      string `json:"raw_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	format, err := deckio.ParseFormat(body.SourceFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.RawText) == "" {
		writeError(w, http.StatusBadRequest, "raw_text is empty")
		return
	}

	parsed := deckio.Parse(format, body.RawText)
	resolved, unresolved := a.resolveLines(r.Context(), parsed)

	parsedJSON, _ := json.Marshal(parsed)
	unresolvedJSON, _ := json.Marshal(nonNil(unresolved))

	uid, tok := callerOwner(r)
	imp, err := a.Queries.CreateImport(r.Context(), db.CreateImportParams{
		SourceFormat: string(format),
		RawText:      body.RawText,
		Parsed:       parsedJSON,
		Unresolved:   unresolvedJSON,
		DeckID:       deck.ID,
		UserID:       uid,
		AnonToken:    tok,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "deck not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to store import")
		}
		return
	}

	writeJSON(w, http.StatusOK, importResponse{
		ImportID:   uuidString(imp.ID),
		Resolved:   nonNil(resolved),
		Unresolved: nonNil(unresolved),
		Rejected:   nonNil(parsed.Rejected),
	})
}

// @spec PORT-006, DECK-060
func (a *API) applyImport(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	importID, ok := parseUUID(chi.URLParam(r, "importId"))
	if !ok {
		writeError(w, http.StatusNotFound, "import not found")
		return
	}
	uid, tok := callerOwner(r)
	imp, err := a.Queries.GetImportForOwner(r.Context(), db.GetImportForOwnerParams{
		ImportID:  importID,
		UserID:    uid,
		AnonToken: tok,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "import not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load import")
		}
		return
	}
	// The import is scoped to its own deck: applying deck A's stored parse into
	// deck B would write the wrong cards and leave a misleading audit trail.
	if imp.DeckID != deck.ID {
		writeError(w, http.StatusNotFound, "import not found")
		return
	}
	// Apply is one-shot. AddDeckCard accumulates quantity on conflict, so a
	// second apply would silently double every card; applied_at records that it
	// has already run. This is a fast path only — the authoritative guard is the
	// conditional MarkImportApplied claimed inside the transaction below, which
	// serializes concurrent applies on the row lock.
	if imp.AppliedAt.Valid {
		writeError(w, http.StatusConflict, "import has already been applied")
		return
	}

	var parsed deckio.ParseResult
	if err := json.Unmarshal(imp.Parsed, &parsed); err != nil {
		writeError(w, http.StatusInternalServerError, "stored import is unreadable")
		return
	}
	resolved, _ := a.resolveLines(r.Context(), parsed)

	// DECK-060: every resolved entry is written in one transaction, so a
	// mid-list failure leaves the deck untouched rather than half-imported.
	tx, err := a.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := a.Queries.WithTx(tx)

	// Claim the import atomically before writing any card. The conditional
	// UPDATE takes the row lock, so a second concurrent apply blocks here until
	// this transaction commits and then sees zero rows affected — 409, no
	// silent quantity doubling.
	claimed, err := qtx.MarkImportApplied(r.Context(), imp.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record import")
		return
	}
	if claimed == 0 {
		writeError(w, http.StatusConflict, "import has already been applied")
		return
	}

	for _, l := range resolved {
		cardID, valid := parseUUID(l.CardID)
		if !valid {
			writeError(w, http.StatusInternalServerError, "resolved line carries an invalid card id")
			return
		}
		params := db.AddDeckCardParams{
			CardID:    cardID,
			Quantity:  int32(l.Quantity),
			Board:     l.Board,
			DeckID:    deck.ID,
			UserID:    uid,
			AnonToken: tok,
		}
		if strings.TrimSpace(l.Category) != "" {
			params.Category = pgtype.Text{String: l.Category, Valid: true}
		}
		if _, err := qtx.AddDeckCard(r.Context(), params); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "deck not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to write imported card")
			}
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit import")
		return
	}

	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	writeJSON(w, http.StatusOK, ld.detailJSON())
}

// @spec PORT-007
func (a *API) exportDeck(w http.ResponseWriter, r *http.Request) {
	deck, ok := a.deckForOwner(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = string(deckio.FormatPlaintext)
	}
	if format != string(deckio.FormatPlaintext) && format != string(deckio.FormatMTGA) {
		writeError(w, http.StatusBadRequest, "export format must be plaintext or mtga")
		return
	}

	ld, err := a.loadDeck(r, deck)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}

	var entries []deckio.Entry
	if ld.commander != nil {
		entries = append(entries, commanderExportEntry(*ld.commander, ld.commanderPrint, ld.haveCommanderPrint))
	}
	if ld.partner != nil {
		entries = append(entries, commanderExportEntry(*ld.partner, ld.partnerPrint, ld.havePartnerPrint))
	}
	for _, e := range ld.entries {
		entries = append(entries, deckio.Entry{
			Quantity:        int(e.Quantity),
			Name:            e.Name,
			Board:           e.Board,
			SetCode:         e.SetCode,
			CollectorNumber: e.CollectorNumber,
		})
	}

	var out string
	if format == string(deckio.FormatMTGA) {
		out = deckio.EmitMTGA(entries)
	} else {
		out = deckio.EmitPlaintext(entries)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

func commanderExportEntry(c db.Card, print db.CardPrint, havePrint bool) deckio.Entry {
	e := deckio.Entry{Quantity: 1, Name: c.Name, Board: deckio.BoardCommand}
	if havePrint {
		e.SetCode = print.SetCode
		e.CollectorNumber = print.CollectorNumber
	}
	return e
}
