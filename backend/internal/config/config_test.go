package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"PORT", "DATABASE_URL", "FRONTEND_URL", "DEV_AUTH", "ANTHROPIC_API_KEY"} {
		t.Setenv(k, "")
	}
}

// @spec PLATFORM-003
func TestLoad_RequiresDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset")
	}
}

// @spec PLATFORM-003
func TestLoad_RequiresFrontendURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error when FRONTEND_URL is unset")
	}
	if !strings.Contains(err.Error(), "FRONTEND_URL") {
		t.Fatalf("expected the error to name FRONTEND_URL, got %v", err)
	}
}

func TestLoad_DevAuthAndPortDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
	t.Setenv("DEV_AUTH", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DevAuth {
		t.Fatal("expected cfg.DevAuth to be true")
	}
	if cfg.Port != "8080" {
		t.Fatalf("expected Port to default to 8080, got %q", cfg.Port)
	}
}
