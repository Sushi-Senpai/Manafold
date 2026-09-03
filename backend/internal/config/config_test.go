package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"PORT", "DATABASE_URL", "FRONTEND_URL", "DEV_AUTH", "ANTHROPIC_API_KEY", "TRUSTED_PROXY_COUNT"} {
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

// @spec PLATFORM-003
func TestLoadCardsync_DoesNotRequireFrontendURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")

	cfg, err := LoadCardsync()
	if err != nil {
		t.Fatalf("LoadCardsync: %v", err)
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("DatabaseURL = %q, want postgres://example", cfg.DatabaseURL)
	}
}

// @spec PLATFORM-003
func TestLoadCardsync_RequiresDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("FRONTEND_URL", "http://localhost:3000")

	_, err := LoadCardsync()
	if err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected the error to name DATABASE_URL, got %v", err)
	}
}

func TestLoad_TrustedProxyCountFailSafe(t *testing.T) {
	// unset -> default 1; a valid non-negative int is honoured (0 included);
	// non-numeric or negative falls back to 1 so a bad value never widens trust.
	cases := []struct {
		raw  string
		want int
	}{
		{"", 1},
		{"2", 2},
		{"0", 0},
		{"garbage", 1},
		{"-3", 1},
	}
	for _, c := range cases {
		clearEnv(t)
		t.Setenv("DATABASE_URL", "postgres://example")
		t.Setenv("FRONTEND_URL", "http://localhost:3000")
		if c.raw != "" {
			t.Setenv("TRUSTED_PROXY_COUNT", c.raw)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(%q): %v", c.raw, err)
		}
		if cfg.TrustedProxyCount != c.want {
			t.Fatalf("TRUSTED_PROXY_COUNT=%q -> %d, want %d", c.raw, cfg.TrustedProxyCount, c.want)
		}
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
