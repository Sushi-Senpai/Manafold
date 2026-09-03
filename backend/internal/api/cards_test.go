package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func cardRouter(a *API) http.Handler {
	r := chi.NewRouter()
	a.RegisterCardRoutes(r)
	return r
}

// @spec CARD-023
func TestSearchCards_UnparseableQuery_Returns400NamingToken(t *testing.T) {
	h := cardRouter(&API{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cards/search?q=cmc%3Anope", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad query = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if body["error"] == "" || !containsToken(body["error"], "cmc:nope") {
		t.Fatalf("error %q does not name the offending token", body["error"])
	}
}

// @spec CARD-020
func TestAutocomplete_EmptyPrefix_ReturnsEmptyList(t *testing.T) {
	h := cardRouter(&API{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cards/autocomplete?q=", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("empty autocomplete = %d, want 200", rec.Code)
	}
	var body struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if body.Names == nil || len(body.Names) != 0 {
		t.Fatalf("names = %v, want []", body.Names)
	}
}

func containsToken(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
