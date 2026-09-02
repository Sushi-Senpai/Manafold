package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	db "manafold-backend/internal/db/generated"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// @spec PLATFORM-002
func TestHealth_OKWithDatabase(t *testing.T) {
	pool := testPool(t)
	handler := New(Deps{Pool: pool, Queries: db.New(pool), DevAuth: true})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
		t.Fatalf("GET /health body = %q, want {\"status\":\"ok\"}", rec.Body.String())
	}
}

// @spec PLATFORM-002
func TestHealth_ServiceUnavailableWithoutDatabase(t *testing.T) {
	handler := New(Deps{Pool: nil, DevAuth: false})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /health with no pool = %d, want 503", rec.Code)
	}
}

// @spec ACCT-003
func TestProtectedRoute_SessionAuth_RejectsUnauthenticatedRequest(t *testing.T) {
	handler := New(Deps{DevAuth: false})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/decks", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/decks = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

// @spec ACCT-001, PLATFORM-005
func TestProtectedRoute_DevAuth_ServesAuthenticatedRequest(t *testing.T) {
	pool := testPool(t)
	handler := New(Deps{Pool: pool, Queries: db.New(pool), DevAuth: true})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/decks", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("DevAuth GET /api/decks = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /api/decks body not JSON: %v (%s)", err, rec.Body.String())
	}
	if _, ok := body["decks"]; !ok {
		t.Fatalf("GET /api/decks response missing \"decks\": %v", body)
	}
}

// @spec DECK-031
func TestPublicDeckRoute_Unknown_Returns404_Unauthenticated(t *testing.T) {
	pool := testPool(t)
	handler := New(Deps{Pool: pool, Queries: db.New(pool), DevAuth: true})

	rec := httptest.NewRecorder()
	// A well-formed but non-existent id: the route is outside the auth group,
	// so this must not 401.
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public/decks/00000000-0000-0000-0000-000000000000", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /public/decks/<unknown> = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
